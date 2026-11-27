package database

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"panel/internal/executor"
	"panel/internal/store"
)

// Run on a Docker-enabled machine with alpine:3.22 pulled:
//
//	go test ./internal/database -run TestRealPublicPort -v
//
// Builds a small gate image from this source, opens a public port and connects with
// the engines' own clients: TLS works (also verifying the certificate), plain
// connections and addresses outside the allowlist are refused.
func TestRealPublicPort(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker")
	}
	if err := exec.Command("docker", "image", "inspect", "alpine:3.22").Run(); err != nil {
		t.Skip("no alpine:3.22 image locally")
	}
	arch, err := exec.Command("docker", "version", "-f", "{{.Server.Arch}}").Output()
	if err != nil {
		t.Skipf("Docker unavailable: %v", err)
	}
	dir := t.TempDir()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "panel"), "./cmd/panel")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+strings.TrimSpace(string(arch)))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build panel: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine:3.22\nCOPY panel /usr/local/bin/panel\nENTRYPOINT [\"/usr/local/bin/panel\"]\n"), 0o644)
	image := fmt.Sprintf("dbgate-test:%d", time.Now().UnixNano())
	if out, err := exec.Command("docker", "build", "-q", "-t", image, dir).CombinedOutput(); err != nil {
		t.Fatalf("build image: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "image", "rm", "-f", image).Run() })

	for _, engine := range []struct{ name, version string }{{"postgres", "18"}, {"mariadb", "11.8"}} {
		t.Run(engine.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			network := fmt.Sprintf("dbpublic-%d", time.Now().UnixNano())
			ex, err := executor.NewDocker(executor.Options{Network: network, LabelPrefix: network})
			if err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			svc := New(ctx, st, ex)
			svc.GateImage = func(context.Context) (string, error) { return image, nil }
			svc.PublicHost = "host.docker.internal"
			var did string
			t.Cleanup(func() {
				cancel()
				svc.Wait()
				c, stop := context.WithTimeout(context.Background(), time.Minute)
				defer stop()
				if did != "" {
					_ = ex.RemoveContainer(c, GateName(did), true)
					_ = ex.RemoveContainer(c, ContainerName(did), true)
					_ = exec.CommandContext(c, "docker", "volume", "rm", VolumeName(did)).Run()
				}
				_ = exec.CommandContext(c, "docker", "network", "rm", network).Run()
				ex.Close()
				st.Close()
			})
			p, _ := st.CreateProject(ctx, "test", "")
			d, err := svc.Create(ctx, input(p, engine.name, engine.version))
			if err != nil {
				t.Fatal(err)
			}
			did = d.ID
			svc.Wait()
			if d, _ = st.GetDBInstance(ctx, did); d.Status != "running" {
				t.Fatalf("provision: %s %s", d.Status, d.Error)
			}
			l, _ := net.Listen("tcp", "127.0.0.1:0")
			port := l.Addr().(*net.TCPAddr).Port
			l.Close()
			if _, err := svc.SetPublicAccess(ctx, did, port, ""); err != nil {
				t.Fatal(err)
			}
			pem, _ := svc.Certificate(ctx, did)
			certDir := t.TempDir()
			os.WriteFile(filepath.Join(certDir, "ca.pem"), []byte(pem), 0o644)
			creds, _ := st.DBInstanceCredentials(ctx, did)
			// connect runs the engine's client in a throwaway container on the host's side.
			connect := func(args ...string) error {
				base := []string{"run", "--rm", "--add-host=host.docker.internal:host-gateway", "-v", certDir + ":/certs:ro"}
				if engine.name == "postgres" {
					base = append(base, "-e", "PGPASSWORD="+creds["password"], "-e", "PGCONNECT_TIMEOUT=5", "postgres:"+engine.version, "psql")
				} else {
					base = append(base, "-e", "MYSQL_PWD="+creds["password"], "mariadb:"+engine.version, "mariadb", "--connect-timeout=5")
				}
				out, err := exec.CommandContext(ctx, "docker", append(base, args...)...).CombinedOutput()
				if err != nil {
					return fmt.Errorf("%v: %s", err, out)
				}
				return nil
			}
			pg := func(mode string, extra ...string) []string {
				conn := fmt.Sprintf("host=host.docker.internal port=%d dbname=app user=app sslmode=%s", port, mode)
				return []string{conn + " " + strings.Join(extra, " "), "-c", "SELECT 1"}
			}
			my := func(ssl ...string) []string {
				return append([]string{"--host=host.docker.internal", fmt.Sprintf("--port=%d", port), "--user=app", "--database=app", "-e", "SELECT 1"}, ssl...)
			}
			var tlsOK, verified, plain []string
			if engine.name == "postgres" {
				tlsOK, verified, plain = pg("require"), pg("verify-full", "sslrootcert=/certs/ca.pem"), pg("disable")
			} else {
				tlsOK, verified, plain = my("--ssl", "--disable-ssl-verify-server-cert"), my("--ssl", "--ssl-ca=/certs/ca.pem", "--ssl-verify-server-cert"), my("--skip-ssl")
			}
			// SetPublicAccess returns once the gate container is started, not once it
			// listens: the first connection may come too early (refused on Linux, cut
			// during the handshake behind a port forwarder). Wait for the gate.
			var tlsErr error
			for deadline := time.Now().Add(45 * time.Second); ; time.Sleep(time.Second) {
				if tlsErr = connect(tlsOK...); tlsErr == nil || time.Now().After(deadline) {
					break
				}
			}
			if tlsErr != nil {
				t.Fatal("TLS connection failed:", tlsErr)
			}
			if err := connect(verified...); err != nil {
				t.Fatal("verified TLS connection failed:", err)
			}
			if err := connect(plain...); err == nil || !strings.Contains(err.Error(), "TLS is required") {
				t.Fatal("plain connection not refused:", err)
			}
			// Docker may rewrite the source address; an allowlist without it refuses all.
			if _, err := svc.SetPublicAccess(ctx, did, port, "203.0.113.0/24"); err != nil {
				t.Fatal(err)
			}
			if err := connect(tlsOK...); err == nil {
				t.Fatal("address outside the allowlist connected")
			}
			if _, err := svc.SetPublicAccess(ctx, did, 0, ""); err != nil {
				t.Fatal(err)
			}
			if err := connect(tlsOK...); err == nil {
				t.Fatal("closed port still accepts connections")
			}
		})
	}
}
