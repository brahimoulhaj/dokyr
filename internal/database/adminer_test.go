package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"panel/internal/brand"
	"panel/internal/executor"
)

func TestSharedAdminerGateway(t *testing.T) {
	s, host, _ := testService(t)
	ctx := context.Background()
	s.AdminerPort = 18081
	address, key, err := s.ensureAdminer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if address != "http://127.0.0.1:18081" || len(key) < 40 {
		t.Fatal("missing private gateway")
	}
	spec := host.specs[0]
	if spec.Kind != executor.KindService || spec.Image != adminerImage || spec.MemoryMB != 256 || len(spec.Ports) != 1 || spec.Ports[0].HostIP != "127.0.0.1" || spec.Env["PANEL_GATEWAY_KEY"] != key {
		t.Fatalf("unsafe gateway: %+v", spec)
	}
	if strings.Contains(strings.Join(spec.Command, " "), key) {
		t.Fatal("secret in process arguments")
	}
	_, sameKey, err := s.ensureAdminer(ctx)
	if err != nil || sameKey != key || len(host.specs) != 1 {
		t.Fatal("gateway was not shared")
	}
	name := brand.Slug + "-adminer"
	if err := host.StopContainer(ctx, name, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ensureAdminer(ctx); err != nil {
		t.Fatal(err)
	}
	c, _ := host.InspectContainer(ctx, name)
	if !c.Running || len(host.specs) != 1 {
		t.Fatal("stopped gateway was not reused")
	}
	// A panel restart rotates the gateway key and replaces the old container.
	s.adminerKey = ""
	_, nextKey, err := s.ensureAdminer(ctx)
	if err != nil || nextKey == key || len(host.specs) != 2 {
		t.Fatal("old gateway survived key rotation")
	}
}
