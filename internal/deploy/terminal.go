package deploy

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"strings"

	"panel/internal/executor"
)

func (s *Service) OpenTerminal(ctx context.Context, appID, shell string, cols, rows uint) (executor.Terminal, error) {
	_, dep, err := s.current(ctx, appID)
	if err != nil {
		return nil, err
	}
	c, err := s.opts.Executor.InspectContainer(ctx, dep.ContainerName)
	if err != nil {
		return nil, err
	}
	if !c.Running {
		return nil, errors.New("start the container before opening a terminal")
	}
	ex, ok := s.opts.Executor.(executor.TerminalExecutor)
	if !ok {
		return nil, errors.New("container terminal is unavailable")
	}
	return ex.OpenTerminal(ctx, c.ID, shell, cols, rows)
}

// HostPaths lists the host paths the app's running container mounts. The saved settings
// do not answer this: they may have changed since the deploy, and the container keeps
// what it was started with. The files of file mounts are left out; they are bind mounts
// too, of content written by the panel under deployments/<app>/files.
func (s *Service) HostPaths(ctx context.Context, appID string) ([]string, error) {
	_, dep, err := s.current(ctx, appID)
	if err != nil {
		return nil, err
	}
	c, err := s.opts.Executor.InspectContainer(ctx, dep.ContainerName)
	if err != nil {
		return nil, err
	}
	return hostPaths(appID, c.Binds), nil
}

func hostPaths(appID string, binds []string) []string {
	own := "/" + path.Join("deployments", appID, "files") + "/"
	var out []string
	for _, b := range binds {
		if !strings.Contains(filepath.ToSlash(b), own) {
			out = append(out, b)
		}
	}
	return out
}
