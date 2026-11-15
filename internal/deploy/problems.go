package deploy

import (
	"context"

	"panel/internal/executor"
	"panel/internal/store"
)

// AppProblems observes all current apps in one Docker call. A Docker outage is an
// error, never an empty healthy result or a fabricated crash for every app.
func (s *Service) AppProblems(ctx context.Context) ([]store.AppRuntimeTarget, error) {
	targets, err := s.opts.Store.AppRuntimeTargets(ctx)
	if err != nil {
		return nil, err
	}
	out := []store.AppRuntimeTarget{}
	if len(targets) == 0 {
		return out, nil
	}
	containers, err := s.opts.Executor.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]executor.Container{}
	for _, c := range containers {
		if c.Kind == executor.KindApp {
			byName[c.Name] = c
		}
	}
	for _, target := range targets {
		c, exists := byName[target.ContainerName]
		target.RuntimeState = "missing"
		if exists && c.ResourceID == target.AppID {
			target.RuntimeState = c.State
			if c.Running && c.State == "running" {
				continue
			}
		}
		out = append(out, target)
	}
	return out, nil
}
