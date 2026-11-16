package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"os"

	"panel/internal/brand"
	"panel/internal/dbgate"
)

// dbGate runs the public entrance of one database instance. Its settings come from
// the environment the panel sets on the gate container: <PREFIX>GATE_LISTEN,
// _TARGET, _ENGINE, _ALLOW (IPs/CIDRs, empty = anyone), _CERT and _KEY (PEM).
func dbGate(ctx context.Context) error {
	get := func(name string) string { return os.Getenv(brand.EnvPrefix() + "GATE_" + name) }
	cert, err := tls.X509KeyPair([]byte(get("CERT")), []byte(get("KEY")))
	if err != nil {
		return errors.New("db-gate: invalid TLS certificate or key")
	}
	allow, err := dbgate.ParseAllow(get("ALLOW"))
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	log.Info("database gate listening", "listen", get("LISTEN"), "engine", get("ENGINE"), "allowlist", len(allow))
	return dbgate.Serve(ctx, dbgate.Config{Listen: get("LISTEN"), Target: get("TARGET"), Engine: get("ENGINE"), Allow: allow, Cert: cert, Log: log})
}
