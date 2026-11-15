package database

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"panel/internal/brand"
	"panel/internal/executor"
)

//go:embed adminer.php
var adminerRouter string

const adminerImage = "adminer:5.5.1-standalone"

// Adminer starts a single private gateway for all instances on this host. The key
// rotates when the panel restarts; an old gateway is replaced before reuse.
func (s *Service) ensureAdminer(ctx context.Context) (string, string, error) {
	s.adminerMu.Lock()
	defer s.adminerMu.Unlock()
	if s.adminerKey == "" {
		key, err := password()
		if err != nil {
			return "", "", err
		}
		s.adminerKey = key
	}
	name := brand.Slug + "-adminer"
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(s.adminerKey+adminerRouter)))
	c, err := s.ex.InspectContainer(ctx, name)
	if err != nil && !errors.Is(err, executor.ErrNotFound) {
		return "", "", err
	}
	if err == nil && c.Labels[brand.Slug+".gateway"] != fingerprint {
		if err := s.ex.RemoveContainer(ctx, c.ID, true); err != nil {
			return "", "", err
		}
		err = executor.ErrNotFound
	}
	if errors.Is(err, executor.ErrNotFound) {
		if err := s.ex.EnsureNetwork(ctx); err != nil {
			return "", "", err
		}
		if err := s.ex.PullImage(ctx, adminerImage, nil, nil); err != nil {
			return "", "", err
		}
		spec := executor.ContainerSpec{
			Name: name, Image: adminerImage, Kind: executor.KindService, ResourceID: "adminer",
			Labels:     map[string]string{brand.Slug + ".gateway": fingerprint},
			Env:        map[string]string{"PANEL_GATEWAY_KEY": s.adminerKey, "PANEL_ROUTER": base64.StdEncoding.EncodeToString([]byte(adminerRouter))},
			Entrypoint: []string{"sh", "-ec"},
			Command:    []string{`printf '%s' "$PANEL_ROUTER" | base64 -d > /tmp/panel-router.php; exec php -S 0.0.0.0:8080 /tmp/panel-router.php`},
			MemoryMB:   256, Restart: executor.RestartUnlessStopped,
		}
		if s.AdminerPort > 0 {
			spec.Ports = []executor.PortBinding{{HostIP: "127.0.0.1", HostPort: s.AdminerPort, ContainerPort: 8080}}
		}
		id, err := s.ex.CreateContainer(ctx, spec)
		if err != nil {
			return "", "", err
		}
		c.ID = id
		c.Running = false
	}
	if !c.Running {
		if err := s.ex.StartContainer(ctx, c.ID); err != nil {
			return "", "", err
		}
	}
	address := "http://" + name + ":8080"
	if s.AdminerPort > 0 {
		address = fmt.Sprintf("http://127.0.0.1:%d", s.AdminerPort)
	}
	return address, s.adminerKey, nil
}

func (s *Service) Adminer(ctx context.Context) (string, string, error) {
	address, key, err := s.ensureAdminer(ctx)
	if err != nil {
		return "", "", err
	}
	// Wait for PHP, so the first click also works immediately after provisioning.
	client := &http.Client{Timeout: time.Second}
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", address, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 403 {
				return address, key, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}
