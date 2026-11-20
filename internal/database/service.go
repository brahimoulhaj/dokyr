package database

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"panel/internal/brand"
	"panel/internal/executor"
	"panel/internal/store"
)

var (
	ErrInvalid   = errors.New("invalid database settings")
	ErrBusy      = errors.New("the database already has an operation in progress")
	instanceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
	sqlName      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
)

type Host interface {
	executor.Executor
	executor.CommandExecutor
}

type Service struct {
	ctx  context.Context
	st   *store.Store
	ex   Host
	mu   sync.Mutex
	busy map[string]bool
	// reading counts the restores using each backup, so it cannot be deleted
	// while a restore into another instance (which holds a different lock) reads it.
	reading       map[string]int
	wg            sync.WaitGroup
	adminerMu     sync.Mutex
	schedulerOnce sync.Once
	adminerKey    string
	BackupTarget  func(store.S3Input) (BackupObjects, error)
	// AdminerPort publishes only on loopback when the panel runs outside Docker.
	AdminerPort int
	// Shorter intervals are useful for tests; production waits for SQL readiness.
	Poll time.Duration
	// GateImage returns the image public-port gates run (the panel's own image);
	// nil or an error means public ports are unavailable.
	GateImage func(context.Context) (string, error)
	// PublicHost is the server's address shown to clients and put in certificates.
	PublicHost string
	// ReservedPorts are host ports a public database port may not take.
	ReservedPorts []int
}

func New(ctx context.Context, st *store.Store, ex Host) *Service {
	return &Service{ctx: ctx, st: st, ex: ex, busy: map[string]bool{}, reading: map[string]int{}, Poll: time.Second}
}

func ContainerName(id string) string { return brand.Slug + "-db-" + strings.ToLower(id) }
func VolumeName(id string) string    { return ContainerName(id) + "-data" }

func Validate(d store.DBInstance) error {
	if !instanceName.MatchString(d.Name) {
		return fmt.Errorf("%w: name must start with a letter and contain up to 48 lowercase letters, digits or hyphens", ErrInvalid)
	}
	if !sqlName.MatchString(d.DatabaseName) || !sqlName.MatchString(d.Username) {
		return fmt.Errorf("%w: database and user must start with a letter and contain up to 48 lowercase letters, digits or underscores", ErrInvalid)
	}
	for _, name := range []string{d.DatabaseName, d.Username} {
		switch name {
		case "postgres", "root", "mysql", "sys", "information_schema", "performance_schema", "template0", "template1", "public":
			return fmt.Errorf("%w: reserved database or user name %s", ErrInvalid, name)
		}
	}
	if math.IsNaN(d.CPULimit) || math.IsInf(d.CPULimit, 0) || d.CPULimit < 0 || d.CPULimit > 1024 || d.MemoryMB < 0 || d.MemoryMB > 1048576 || (d.MemoryMB > 0 && d.MemoryMB < 128) {
		return fmt.Errorf("%w: CPU must be 0–1024 cores; memory must be 0 (unlimited) or 128–1048576 MiB", ErrInvalid)
	}
	_, err := engineDriver(d.Engine, d.Version)
	return err
}

func password() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func (s *Service) Create(ctx context.Context, in store.DBInstance) (store.DBInstance, error) {
	d, _, err := s.CreateWithOperation(ctx, in)
	return d, err
}

func (s *Service) CreateWithOperation(ctx context.Context, in store.DBInstance) (store.DBInstance, store.DBOperation, error) {
	if err := Validate(in); err != nil {
		return store.DBInstance{}, store.DBOperation{}, err
	}
	root, err := password()
	if err != nil {
		return store.DBInstance{}, store.DBOperation{}, err
	}
	app, err := password()
	if err != nil {
		return store.DBInstance{}, store.DBOperation{}, err
	}
	d, err := s.st.CreateDBInstance(ctx, in, map[string]string{"root_password": root, "password": app})
	if err != nil {
		return d, store.DBOperation{}, err
	}
	op, err := s.Enqueue(ctx, d.ID, "create")
	return d, op, err
}

// Enqueue makes the accepted operation durable. Work uses the service context,
// so navigating away or disconnecting the browser never cancels it.
func (s *Service) Enqueue(ctx context.Context, id, action string) (store.DBOperation, error) {
	if action != "create" && action != "start" && action != "stop" && action != "restart" && action != "delete" {
		return store.DBOperation{}, ErrInvalid
	}
	if !s.acquire(id) {
		return store.DBOperation{}, ErrBusy
	}
	d, err := s.st.GetDBInstance(ctx, id)
	if err != nil {
		s.release(id)
		return store.DBOperation{}, err
	}
	op, err := s.st.CreateDBOperation(ctx, d, action)
	if err != nil {
		s.release(id)
		if errors.Is(err, store.ErrConflict) {
			err = ErrBusy
		}
		return op, err
	}
	s.launch(op)
	return op, nil
}

func (s *Service) acquire(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[id] {
		return false
	}
	s.busy[id] = true
	return true
}
func (s *Service) release(id string) { s.mu.Lock(); delete(s.busy, id); s.mu.Unlock() }

// claimBackup marks a backup as read by a restore; unclaimBackup undoes one claim.
func (s *Service) claimBackup(id string) {
	if id != "" {
		s.mu.Lock()
		s.reading[id]++
		s.mu.Unlock()
	}
}
func (s *Service) unclaimBackup(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reading[id]--; s.reading[id] <= 0 {
		delete(s.reading, id)
	}
}
func (s *Service) backupInUse(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reading[id] > 0
}

// restoredBackup names the backup an operation reads, if it is a restore.
func (s *Service) restoredBackup(op store.DBOperation) string {
	if op.Action != "restore" && op.Action != "create" {
		return ""
	}
	p, err := s.st.DBOperationPayload(s.ctx, op.ID)
	if err != nil {
		return ""
	}
	if op.Action == "restore" {
		return p["backup_id"]
	}
	return p["restore_backup_id"]
}

// MoveProject is an immediate metadata change serialized with SQL/lifecycle work.
func (s *Service) MoveProject(ctx context.Context, id, projectID string) (store.DBInstance, error) {
	if strings.TrimSpace(projectID) == "" {
		return store.DBInstance{}, fmt.Errorf("%w: select a project", ErrInvalid)
	}
	if !s.acquire(id) {
		return store.DBInstance{}, ErrBusy
	}
	defer s.release(id)
	d, err := s.st.MoveDBInstance(ctx, id, projectID)
	if errors.Is(err, store.ErrDBInstanceBusy) {
		err = ErrBusy
	}
	return d, err
}

func (s *Service) launch(op store.DBOperation) {
	backup := s.restoredBackup(op)
	s.claimBackup(backup)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.unclaimBackup(backup)
		defer s.release(op.InstanceID)
		ctx, cancel := context.WithTimeout(s.ctx, s.operationTimeout(op))
		defer cancel()
		_ = s.runOperation(ctx, op)
	}()
}

func (s *Service) runOperation(ctx context.Context, op store.DBOperation) error {
	if err := s.st.StartDBOperation(ctx, op.ID); err != nil {
		return err
	}
	var err error
	switch op.Action {
	case "create", "start":
		err = s.provision(ctx, op.InstanceID)
		if err == nil && op.Action == "create" {
			err = s.restoreAfterCreate(ctx, op)
		}
		if err == nil {
			err = s.syncGate(ctx, op.InstanceID)
		}
	case "stop":
		err = s.stop(ctx, op.InstanceID)
	case "restart":
		err = s.stop(ctx, op.InstanceID)
		if err == nil {
			err = s.st.SetDBInstanceState(ctx, op.InstanceID, store.StateRunning, "provisioning", "")
		}
		if err == nil {
			err = s.provision(ctx, op.InstanceID)
		}
		if err == nil {
			err = s.syncGate(ctx, op.InstanceID)
		}
	case "delete":
		// Deletion may have completed just before shutdown, leaving the job unfinished.
		if _, getErr := s.st.GetDBInstance(ctx, op.InstanceID); errors.Is(getErr, store.ErrNotFound) {
			break
		}
		err = s.deleteInstance(ctx, op.InstanceID)
	case "backup", "restore":
		err = s.backupOperation(ctx, op)
	case "database_create", "user_create", "grant_update", "database_delete", "user_delete", "password_rotate", "catalog_sync":
		err = s.manageOperation(ctx, op)
	}
	// Keep interrupted work unfinished: startup resumes the same operation ID.
	if s.ctx.Err() != nil {
		return err
	}
	save, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reason := ""
	if err != nil {
		reason = "The database operation failed; check Docker and retry"
		// These functions return sanitized phase errors, never raw SQL/credentials.
		if op.Action == "create" || op.Action == "start" {
			reason = err.Error()
		}
		if managementAction(op.Action) {
			reason = err.Error()
		}
		if d, getErr := s.st.GetDBInstance(save, op.InstanceID); getErr == nil && !managementAction(op.Action) {
			_ = s.st.SetDBInstanceState(save, op.InstanceID, d.DesiredState, "failed", reason)
		}
	}
	if finishErr := s.st.FinishDBOperation(save, op.ID, reason); finishErr != nil {
		return finishErr
	}
	if err != nil && op.Action == "backup" {
		// Alertable: scheduled backups fail with no one watching.
		detail := op.Detail
		if done, getErr := s.st.GetDBOperation(save, op.ID); getErr == nil && done.Error != "" {
			detail += ": " + done.Error
		}
		_ = s.st.AddEvent(save, store.Event{Actor: "system", Action: "backup.failed", TargetType: "database", TargetID: op.InstanceID, TargetName: op.InstanceName, Detail: detail})
	}
	if op.Action == "backup" {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s.finishPolicyBackup(cleanup, op, err)
	}
	return err
}

func (s *Service) provision(ctx context.Context, id string) error {
	d, err := s.st.GetDBInstance(ctx, id)
	if err != nil {
		return errors.New("the database record could not be read")
	}
	creds, err := s.st.DBInstanceCredentials(ctx, id)
	if err != nil {
		return errors.New("database credentials could not be decrypted")
	}
	driver, err := engineDriver(d.Engine, d.Version)
	if err != nil {
		return err
	}
	if err := s.ex.EnsureNetwork(ctx); err != nil {
		return errors.New("Docker network is unavailable; start the instance to retry")
	}
	c, err := s.ex.InspectContainer(ctx, ContainerName(id))
	if errors.Is(err, executor.ErrNotFound) {
		spec := driver.Spec(d, creds)
		spec.Name, spec.Kind, spec.ResourceID = ContainerName(id), executor.KindDatabase, id
		spec.CPUs, spec.MemoryMB, spec.Restart = d.CPULimit, d.MemoryMB, executor.RestartUnlessStopped
		if err := s.ex.PullImage(ctx, spec.Image, nil, nil); err != nil {
			return errors.New("the database image could not be downloaded; start the instance to retry")
		}
		cid, err := s.ex.CreateContainer(ctx, spec)
		if err != nil {
			return errors.New("the database container could not be created; start the instance to retry")
		}
		c.ID = cid
	} else if err != nil {
		return errors.New("Docker is unavailable; start the instance to retry")
	} else if c.Kind != executor.KindDatabase || c.ResourceID != id {
		return errors.New("the database container name belongs to another workload")
	}
	if !c.Running {
		if err := s.ex.StartContainer(ctx, c.ID); err != nil {
			return errors.New("the database container could not start; inspect its logs")
		}
	}
	ready, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		observed, err := s.ex.InspectContainer(ready, c.ID)
		if err == nil && (observed.State == "exited" || observed.State == "dead") {
			return errors.New("the database container exited before becoming ready; inspect its logs")
		}
		check, stop := context.WithTimeout(ready, 5*time.Second)
		err = driver.Ready(check, s.ex, c.ID, d, creds)
		stop()
		if err == nil {
			break
		}
		select {
		case <-ready.Done():
			return errors.New("database readiness timed out; inspect its logs and start it to retry")
		case <-time.After(s.Poll):
		}
	}
	if err := driver.Initialize(ready, s.ex, c.ID, d, creds); err != nil {
		return errors.New("database user initialization failed; start the instance to retry")
	}
	if err := s.st.SetDBInstanceState(ctx, id, store.StateRunning, "running", ""); err != nil {
		return errors.New("database state could not be saved")
	}
	return nil
}

// Recover resumes the same persisted operations, then repairs legacy interrupted
// states or missing containers. Running healthy instances do not create new jobs.
func (s *Service) Recover(ctx context.Context) error {
	ops, err := s.st.ActiveDBOperations(ctx)
	if err != nil {
		return err
	}
	pending := map[string]bool{}
	for _, op := range ops {
		if op.Action == "create" && op.Status == "running" {
			// A restore into a new instance never replays over partially loaded data.
			if p, err := s.st.DBOperationPayload(ctx, op.ID); err == nil && p["phase"] == "restoring" {
				reason := "Restore was interrupted. This instance may contain partial data; delete it and restore the backup again."
				if err := s.st.FinishDBOperation(ctx, op.ID, reason); err != nil {
					return err
				}
				if d, err := s.st.GetDBInstance(ctx, op.InstanceID); err == nil {
					_ = s.st.SetDBInstanceState(ctx, d.ID, d.DesiredState, "failed", reason)
				}
				pending[op.InstanceID] = true
				continue
			}
		}
		if op.Action == "restore" && op.Status == "running" {
			if err := s.st.FinishDBOperation(ctx, op.ID, "Restore was interrupted. Its new database may contain partial data; inspect it and restore again using a different name."); err != nil {
				return err
			}
			continue
		}
		pending[op.InstanceID] = true
		if s.acquire(op.InstanceID) {
			s.launch(op)
		}
	}
	list, err := s.st.ListDBInstances(ctx)
	if err != nil {
		return err
	}
	for _, d := range list {
		if pending[d.ID] {
			continue
		}
		runtime := s.Runtime(ctx, d.ID)
		if d.PublicPort > 0 && d.DesiredState == store.StateRunning && runtime == "running" {
			// Recreates the gate after a panel update (new image) or a lost container.
			_ = s.syncGate(ctx, d.ID)
		}
		if d.DesiredState == store.StateRunning && d.Status != "failed" && (d.Status == "provisioning" || runtime == "missing" || runtime == "exited" || runtime == "dead") {
			if _, err := s.Enqueue(ctx, d.ID, "start"); err != nil && !errors.Is(err, ErrBusy) {
				return err
			}
		}
		if d.DesiredState == store.StateStopped && (d.Status == "stopping" || runtime == "running" || runtime == "restarting") {
			if _, err := s.Enqueue(ctx, d.ID, "stop"); err != nil && !errors.Is(err, ErrBusy) {
				return err
			}
		}
	}
	return nil
}

func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) Action(ctx context.Context, id, action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return ErrInvalid
	}
	_, err := s.Enqueue(ctx, id, action)
	return err
}

// stop and deleteInstance run with the instance lock already held.
func (s *Service) stop(ctx context.Context, id string) error {
	if err := s.st.SetDBInstanceState(ctx, id, store.StateStopped, "stopping", ""); err != nil {
		return err
	}
	// The public gate goes first, so nothing is accepted for a stopping database.
	if err := s.ex.RemoveContainer(ctx, GateName(id), true); err != nil {
		return errors.New("the public gate could not be removed")
	}
	c, err := s.ex.InspectContainer(ctx, ContainerName(id))
	if err != nil && !errors.Is(err, executor.ErrNotFound) {
		return errors.New("Docker is unavailable")
	}
	if err == nil && (c.Running || c.State == "restarting" || c.State == "paused") {
		if err := s.ex.StopContainer(ctx, c.ID, 30*time.Second); err != nil {
			return errors.New("the database could not be stopped")
		}
	}
	return s.st.SetDBInstanceState(ctx, id, store.StateStopped, "stopped", "")
}

// Delete is a synchronous convenience for callers outside the HTTP API.
// The API uses Enqueue so removal is observable globally and survives navigation.
func (s *Service) Delete(ctx context.Context, id string) error {
	if !s.acquire(id) {
		return ErrBusy
	}
	defer s.release(id)
	d, err := s.st.GetDBInstance(ctx, id)
	if err != nil {
		return err
	}
	op, err := s.st.CreateDBOperation(ctx, d, "delete")
	if err != nil {
		return err
	}
	return s.runOperation(ctx, op)
}

func (s *Service) deleteInstance(ctx context.Context, id string) error {
	if err := s.stop(ctx, id); err != nil {
		return err
	}
	if err := s.ex.RemoveContainer(ctx, ContainerName(id), false); err != nil {
		return errors.New("the database container could not be removed")
	}
	// Data volume is retained, as before. Operation snapshots have no FK to this row.
	return s.st.DeleteDBInstance(ctx, id)
}

func (s *Service) Runtime(ctx context.Context, id string) string {
	c, err := s.ex.InspectContainer(ctx, ContainerName(id))
	if errors.Is(err, executor.ErrNotFound) {
		return "missing"
	}
	if err != nil {
		return "unavailable"
	}
	return c.State
}

func (s *Service) Logs(ctx context.Context, id string) ([]executor.LogLine, error) {
	if _, err := s.st.GetDBInstance(ctx, id); err != nil {
		return nil, err
	}
	lines := []executor.LogLine{}
	err := s.ex.Logs(ctx, ContainerName(id), executor.LogOptions{Tail: 200}, func(l executor.LogLine) { lines = append(lines, l) })
	return lines, err
}

// operationTimeout gives dumps and restores hours; lifecycle and SQL work minutes.
func (s *Service) operationTimeout(op store.DBOperation) time.Duration {
	switch op.Action {
	case "backup", "restore":
		return 6 * time.Hour
	case "create":
		if p, err := s.st.DBOperationPayload(s.ctx, op.ID); err == nil && p["restore_backup_id"] != "" {
			return 6 * time.Hour
		}
	}
	return 10 * time.Minute
}

// restoreAfterCreate loads a whole-instance backup once a new instance is running.
// The phase is recorded first, so a restart fails the job instead of replaying it.
func (s *Service) restoreAfterCreate(ctx context.Context, op store.DBOperation) error {
	p, err := s.st.DBOperationPayload(ctx, op.ID)
	if err != nil {
		return errors.New("operation input could not be decrypted")
	}
	if p["restore_backup_id"] == "" {
		return nil
	}
	p["phase"] = "restoring"
	if err := s.st.SetDBOperationPayload(ctx, op.ID, p); err != nil {
		return errors.New("restore progress could not be saved")
	}
	d, err := s.st.GetDBInstance(ctx, op.InstanceID)
	if err != nil {
		return errors.New("the database record could not be read")
	}
	return s.restoreInstance(ctx, d, p)
}

// syncGate starts or refreshes the public gate of a running instance.
func (s *Service) syncGate(ctx context.Context, id string) error {
	d, err := s.st.GetDBInstance(ctx, id)
	if err != nil || d.PublicPort == 0 {
		return nil
	}
	// A gate problem never fails the database itself: it is shown on the instance.
	if err := s.ensureGate(ctx, d, false); err != nil {
		reason := "The database is running, but its public port could not be opened: " + strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": ")
		_ = s.st.SetDBInstanceState(ctx, id, d.DesiredState, d.Status, reason)
	}
	return nil
}
