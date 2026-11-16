package database

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"testing"

	"panel/internal/brand"
)

func TestPublicAccess(t *testing.T) {
	svc, h, p := testService(t)
	ctx := context.Background()
	d, err := svc.Create(ctx, input(p, "postgres", "18"))
	if err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	if _, err := svc.SetPublicAccess(ctx, d.ID, 15432, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("opened without a gate image", err)
	}
	svc.GateImage = func(context.Context) (string, error) { return "panel:test", nil }
	svc.PublicHost = "db.example.com"
	svc.ReservedPorts = []int{8443}
	for _, port := range []int{80, 8443, 70000} {
		if _, err := svc.SetPublicAccess(ctx, d.ID, port, ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("port %d accepted: %v", port, err)
		}
	}
	if _, err := svc.SetPublicAccess(ctx, d.ID, 15432, "not-an-ip"); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad allowlist accepted", err)
	}
	access, _ := svc.PublicAccess(ctx, d.ID)
	if access.Port != 0 || access.SuggestedPort != 5432 || !access.Available {
		t.Fatalf("initial access %+v", access)
	}

	d, err = svc.SetPublicAccess(ctx, d.ID, 15432, "203.0.113.7\n10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	if d.PublicPort != 15432 || d.PublicAllow != "203.0.113.7,10.0.0.0/8" {
		t.Fatalf("saved %+v", d)
	}
	spec := h.specs[len(h.specs)-1]
	env := func(k string) string { return spec.Env[brand.EnvPrefix()+"GATE_"+k] }
	if spec.Name != GateName(d.ID) || spec.Image != "panel:test" || spec.Command[0] != "db-gate" || spec.Ports[0].HostPort != 15432 || spec.Ports[0].ContainerPort != 5432 ||
		env("TARGET") != ContainerName(d.ID)+":5432" || env("ALLOW") != "203.0.113.7,10.0.0.0/8" || env("ENGINE") != "postgres" {
		t.Fatalf("gate spec %+v", spec)
	}
	if _, err := tls.X509KeyPair([]byte(env("CERT")), []byte(env("KEY"))); err != nil {
		t.Fatal("gate certificate", err)
	}
	pem, err := svc.Certificate(ctx, d.ID)
	if err != nil || !strings.Contains(pem, "BEGIN CERTIFICATE") || strings.Contains(pem, "PRIVATE") {
		t.Fatal("certificate download", err)
	}
	if c, err := h.InspectContainer(ctx, GateName(d.ID)); err != nil || !c.Running {
		t.Fatal("gate not running", err)
	}

	// Another instance cannot take the same port; the suggestion moves on.
	in := input(p, "postgres", "18")
	in.Name = "second"
	other, _ := svc.Create(ctx, in)
	svc.Wait()
	if _, err := svc.SetPublicAccess(ctx, other.ID, 15432, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("duplicate port accepted", err)
	}

	// Stopping removes the gate; starting brings it back with the same certificate.
	if err := svc.Action(ctx, d.ID, "stop"); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	if _, err := h.InspectContainer(ctx, GateName(d.ID)); err == nil {
		t.Fatal("gate kept for a stopped instance")
	}
	if _, err := svc.SetPublicAccess(ctx, d.ID, 15433, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("opened on a stopped instance", err)
	}
	if err := svc.Action(ctx, d.ID, "start"); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	if c, err := h.InspectContainer(ctx, GateName(d.ID)); err != nil || !c.Running {
		t.Fatal("gate not restored on start", err)
	}
	if h.specs[len(h.specs)-1].Env[brand.EnvPrefix()+"GATE_CERT"] != env("CERT") {
		t.Fatal("certificate changed across restarts")
	}

	// Closing removes the gate and frees the port.
	if d, err = svc.SetPublicAccess(ctx, d.ID, 0, ""); err != nil || d.PublicPort != 0 {
		t.Fatal(err)
	}
	if _, err := h.InspectContainer(ctx, GateName(d.ID)); err == nil {
		t.Fatal("gate kept after closing")
	}
	if _, err := svc.SetPublicAccess(ctx, other.ID, 15432, ""); err != nil {
		t.Fatal("freed port not reusable", err)
	}
}
