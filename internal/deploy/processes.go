package deploy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"panel/internal/executor"
	"panel/internal/store"
)

// Labels on app containers: the deployment that made the container, and, on the
// container of an extra process, the process name.
const (
	labelDeployment = "deployment"
	labelProcess    = "process"
)

// processContainerName is the main container's name with the process name after it.
func processContainerName(appID, deploymentID, process string) string {
	return containerName(appID, deploymentID) + "-" + process
}

// startProcesses creates and starts one container per extra process of the app, from the
// deployment's image with the main container's environment, mounts and limits, and waits
// StartGrace for all of them to stay up. The ids it returns are of every container it
// created, also when it fails, so the caller can remove them.
func (s *Service) startProcesses(ctx context.Context, app store.App, dep store.Deployment, image string, mounts []executor.Mount, logw *logWriter) ([]string, error) {
	ex := s.opts.Executor
	var ids []string
	for _, p := range app.Processes {
		name := processContainerName(app.ID, dep.ID, p.Name)
		logw.printf("Starting process %s: %s", p.Name, p.Command)
		id, err := ex.CreateContainer(ctx, executor.ContainerSpec{
			Name:       name,
			Image:      image,
			Kind:       executor.KindApp,
			ResourceID: app.ID,
			Labels:     map[string]string{labelDeployment: dep.ID, labelProcess: p.Name},
			CPUs:       app.CPULimit,
			MemoryMB:   app.MemoryMB,
			Env:        containerEnv(app, dep.Port),
			Entrypoint: []string{"/bin/sh", "-c"},
			Command:    []string{p.Command},
			Mounts:     mounts,
		})
		if err != nil {
			return ids, fmt.Errorf("process %s: %w", p.Name, err)
		}
		ids = append(ids, id)
		if err := ex.StartContainer(ctx, id); err != nil {
			return ids, fmt.Errorf("process %s: %w", p.Name, err)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	start := time.Now()
	for {
		for i, id := range ids {
			if err := s.checkAlive(ctx, id, logw); err != nil {
				return ids, fmt.Errorf("process %s: %w", app.Processes[i].Name, err)
			}
		}
		left := s.opts.StartGrace - time.Since(start)
		if left <= 0 {
			return ids, nil
		}
		if err := sleep(ctx, min(250*time.Millisecond, left)); err != nil {
			return ids, err
		}
	}
}

// processContainers returns the containers of the extra processes of one deployment.
func (s *Service) processContainers(ctx context.Context, appID, deploymentID string) ([]executor.Container, error) {
	list, err := s.opts.Executor.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	var out []executor.Container
	for _, c := range list {
		if c.Kind == executor.KindApp && c.ResourceID == appID && c.Labels[labelDeployment] == deploymentID && c.Labels[labelProcess] != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

// eachProcess runs step on every process container of the deployment. The main container
// decides whether the action worked: a process that fails is logged only.
func (s *Service) eachProcess(ctx context.Context, app store.App, deploymentID, what string, step func(executor.Container) error) {
	list, err := s.processContainers(ctx, app.ID, deploymentID)
	if err != nil {
		s.opts.Log.Error(what+" processes", "app", app.Name, "error", err)
		return
	}
	for _, c := range list {
		if err := step(c); err != nil && !errors.Is(err, executor.ErrNotFound) {
			s.opts.Log.Error(what+" process", "app", app.Name, "process", c.Labels[labelProcess], "error", err)
		}
	}
}

// ProcessState is what the container of an extra process is doing.
type ProcessState struct {
	Name  string `json:"name"`
	State string `json:"state"` // running, exited, restarting, …; "none" until a deployment starts it
}

// ProcessStates reports on each extra process of the app, in the app's order.
func (s *Service) ProcessStates(ctx context.Context, app store.App) []ProcessState {
	out := make([]ProcessState, 0, len(app.Processes))
	if len(app.Processes) == 0 {
		return out
	}
	states := map[string]string{}
	unknown := false
	if app.CurrentDeploymentID != "" {
		list, err := s.processContainers(ctx, app.ID, app.CurrentDeploymentID)
		unknown = err != nil
		for _, c := range list {
			states[c.Labels[labelProcess]] = c.State
		}
	}
	for _, p := range app.Processes {
		state := states[p.Name]
		switch {
		case state == "" && unknown:
			state = "unknown"
		case state == "":
			state = "none"
		}
		out = append(out, ProcessState{Name: p.Name, State: state})
	}
	return out
}

// processProblem names the first extra process of the deployment that is not running.
func (s *Service) processProblem(ctx context.Context, appID, mainContainer string) string {
	list, err := s.opts.Executor.ListContainers(ctx)
	if err != nil {
		return ""
	}
	for _, c := range list {
		name := c.Labels[labelProcess]
		if c.Kind != executor.KindApp || c.ResourceID != appID || name == "" || c.Name != mainContainer+"-"+name {
			continue
		}
		switch {
		case c.State == "restarting":
			return fmt.Sprintf("process %s keeps crashing and restarting", name)
		case !c.Running && c.OOMKilled:
			return fmt.Sprintf("process %s was killed for exceeding its memory limit", name)
		case !c.Running:
			return fmt.Sprintf("process %s stopped (exit code %d)", name, c.ExitCode)
		}
	}
	return ""
}
