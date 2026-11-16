package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"panel/internal/brand"
	"panel/internal/dbgate"
	"panel/internal/executor"
	"panel/internal/store"
)

// GateName is the container that publishes an instance's public port.
func GateName(id string) string { return ContainerName(id) + "-public" }

func enginePort(engine string) int {
	if engine == "postgres" {
		return 5432
	}
	return 3306
}

const gateSpecLabel = "dbgate.spec"

// PublicAccess is what the settings form shows: the saved state plus a free port.
type PublicAccess struct {
	Port          int    `json:"port"` // 0 = off
	Allow         string `json:"allow"`
	SuggestedPort int    `json:"suggested_port"`
	Host          string `json:"host"`      // where clients connect
	Available     bool   `json:"available"` // false when the gate image is unknown
	Gate          string `json:"gate"`      // gate container state: running, missing, …
}

// SuggestPort prefers the engine's usual port, then 15432/13306 and up.
func (s *Service) SuggestPort(ctx context.Context, d store.DBInstance) int {
	used, _ := s.st.UsedPublicPorts(ctx, d.ID)
	if d.PublicPort > 0 {
		return d.PublicPort
	}
	p := enginePort(d.Engine)
	if !used[p] && !s.reserved(p) {
		return p
	}
	for p = 10000 + p; p < 65535; p++ {
		if !used[p] && !s.reserved(p) {
			return p
		}
	}
	return 0
}

func (s *Service) reserved(port int) bool {
	for _, r := range s.ReservedPorts {
		if r == port {
			return true
		}
	}
	return false
}

func (s *Service) PublicAccess(ctx context.Context, id string) (PublicAccess, error) {
	d, err := s.st.GetDBInstance(ctx, id)
	if err != nil {
		return PublicAccess{}, err
	}
	out := PublicAccess{Port: d.PublicPort, Allow: d.PublicAllow, SuggestedPort: s.SuggestPort(ctx, d), Host: s.PublicHost, Gate: "missing"}
	if s.GateImage != nil {
		_, err := s.GateImage(ctx)
		out.Available = err == nil
	}
	if c, err := s.ex.InspectContainer(ctx, GateName(id)); err == nil {
		out.Gate = c.State
	}
	return out, nil
}

// SetPublicAccess opens (port > 0) or closes (port 0) an instance's public port.
// Opening needs a running instance; the gate is recreated with every change.
func (s *Service) SetPublicAccess(ctx context.Context, id string, port int, allowText string) (store.DBInstance, error) {
	allow, err := dbgate.ParseAllow(allowText)
	if err != nil {
		return store.DBInstance{}, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	normalized := make([]string, len(allow))
	for i, p := range allow {
		normalized[i] = p.String()
		if p.Bits() == p.Addr().BitLen() {
			normalized[i] = p.Addr().String()
		}
	}
	if port != 0 {
		if port < 1024 || port > 65535 {
			return store.DBInstance{}, fmt.Errorf("%w: choose a port between 1024 and 65535", ErrInvalid)
		}
		if s.reserved(port) {
			return store.DBInstance{}, fmt.Errorf("%w: port %d is used by the panel", ErrInvalid, port)
		}
		if s.GateImage == nil {
			return store.DBInstance{}, fmt.Errorf("%w: public ports need the panel to run in its container", ErrInvalid)
		}
		if _, err := s.GateImage(ctx); err != nil {
			return store.DBInstance{}, fmt.Errorf("%w: public ports need the panel to run in its container", ErrInvalid)
		}
	}
	if !s.acquire(id) {
		return store.DBInstance{}, ErrBusy
	}
	defer s.release(id)
	d, err := s.st.GetDBInstance(ctx, id)
	if err != nil {
		return d, err
	}
	if port != 0 {
		used, err := s.st.UsedPublicPorts(ctx, id)
		if err != nil {
			return d, err
		}
		if used[port] {
			return d, fmt.Errorf("%w: port %d is already used by another database", ErrInvalid, port)
		}
		if d.Status != "running" || d.DesiredState != store.StateRunning {
			return d, fmt.Errorf("%w: start the instance before opening a public port", ErrInvalid)
		}
		if err := s.ensureCertificate(ctx, d); err != nil {
			return d, err
		}
	}
	previousPort, previousAllow := d.PublicPort, d.PublicAllow
	if err := s.st.SetDBInstancePublic(ctx, id, port, strings.Join(normalized, ",")); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return d, fmt.Errorf("%w: port %d is already used by another database", ErrInvalid, port)
		}
		return d, err
	}
	d, _ = s.st.GetDBInstance(ctx, id)
	if port == 0 {
		if err := s.ex.RemoveContainer(ctx, GateName(id), true); err != nil {
			return d, errors.New("the public port could not be closed; check Docker and retry")
		}
		return d, nil
	}
	if err := s.ensureGate(ctx, d, true); err != nil {
		// Leave the previous settings (and its gate) in place.
		_ = s.st.SetDBInstancePublic(ctx, id, previousPort, previousAllow)
		if previous, e := s.st.GetDBInstance(ctx, id); e == nil && previous.PublicPort > 0 {
			_ = s.ensureGate(ctx, previous, true)
		}
		return d, err
	}
	return d, nil
}

// ensureCertificate creates the gate's self-signed certificate once per instance.
func (s *Service) ensureCertificate(ctx context.Context, d store.DBInstance) error {
	creds, err := s.st.DBInstanceCredentials(ctx, d.ID)
	if err != nil {
		return err
	}
	if creds["tls_cert"] != "" && creds["tls_key"] != "" {
		return nil
	}
	cert, key, err := dbgate.GenerateCertificate([]string{s.PublicHost})
	if err != nil {
		return err
	}
	creds["tls_cert"], creds["tls_key"] = string(cert), string(key)
	return s.st.SetDBInstanceCredentials(ctx, d.ID, creds)
}

// Certificate returns the public gate's certificate (PEM), for verify-full clients.
func (s *Service) Certificate(ctx context.Context, id string) (string, error) {
	creds, err := s.st.DBInstanceCredentials(ctx, id)
	if err != nil {
		return "", err
	}
	if creds["tls_cert"] == "" {
		return "", store.ErrNotFound
	}
	return creds["tls_cert"], nil
}

// ensureGate makes the gate container match the saved settings. An unchanged, running
// gate is left alone unless force is set; a changed one (new image after an update,
// new port or allowlist) is replaced.
func (s *Service) ensureGate(ctx context.Context, d store.DBInstance, force bool) error {
	if d.PublicPort == 0 {
		return s.ex.RemoveContainer(ctx, GateName(d.ID), true)
	}
	if s.GateImage == nil {
		return errors.New("public ports need the panel to run in its container")
	}
	image, err := s.GateImage(ctx)
	if err != nil {
		return errors.New("public ports need the panel to run in its container")
	}
	creds, err := s.st.DBInstanceCredentials(ctx, d.ID)
	if err != nil || creds["tls_cert"] == "" {
		return errors.New("the public port's TLS certificate is missing; open the port again")
	}
	port := enginePort(d.Engine)
	prefix := brand.EnvPrefix() + "GATE_"
	spec := executor.ContainerSpec{
		Name: GateName(d.ID), Image: image, Kind: executor.KindDatabase, ResourceID: d.ID,
		Command: []string{"db-gate"},
		Env: map[string]string{
			prefix + "LISTEN": ":" + strconv.Itoa(port),
			prefix + "TARGET": ContainerName(d.ID) + ":" + strconv.Itoa(port),
			prefix + "ENGINE": d.Engine,
			prefix + "ALLOW":  d.PublicAllow,
			prefix + "CERT":   creds["tls_cert"],
			prefix + "KEY":    creds["tls_key"],
		},
		Ports:    []executor.PortBinding{{HostPort: d.PublicPort, ContainerPort: port}},
		CPUs:     0.25,
		MemoryMB: 64,
		Restart:  executor.RestartUnlessStopped,
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s|%s", image, d.PublicPort, d.PublicAllow, d.Engine, creds["tls_cert"])))
	spec.Labels = map[string]string{gateSpecLabel: hex.EncodeToString(sum[:8])}
	if c, err := s.ex.InspectContainer(ctx, spec.Name); err == nil {
		if !force && c.Running && c.Labels[gateSpecLabel] == spec.Labels[gateSpecLabel] {
			return nil
		}
		if err := s.ex.RemoveContainer(ctx, c.ID, true); err != nil {
			return errors.New("the previous public gate could not be removed")
		}
	} else if !errors.Is(err, executor.ErrNotFound) {
		return errors.New("Docker is unavailable")
	}
	// The panel's own image (by ID) is already on the host; a named one may need a
	// pull. A failed pull is fine for a local-only image: creation reports a real miss.
	if !strings.HasPrefix(image, "sha256:") {
		_ = s.ex.PullImage(ctx, image, nil, nil)
	}
	cid, err := s.ex.CreateContainer(ctx, spec)
	if err != nil {
		return fmt.Errorf("%w: the public gate could not be created; port %d may already be in use on the server", ErrInvalid, d.PublicPort)
	}
	if err := s.ex.StartContainer(ctx, cid); err != nil {
		_ = s.ex.RemoveContainer(ctx, cid, true)
		return fmt.Errorf("%w: port %d is already in use on the server; choose another one", ErrInvalid, d.PublicPort)
	}
	return nil
}
