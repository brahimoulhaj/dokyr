package registry

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"panel/internal/executor"
	"panel/internal/store"
)

// TestDockerCLI pushes and pulls with the docker CLI: docker login against the panel's
// token endpoint (Basic auth), a push token pushes, a pull token cannot. The registry is
// reached on 127.0.0.1, which Docker allows over plain HTTP.
func TestDockerCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("no docker CLI")
	}
	ctx := context.Background()
	name := fmt.Sprintf("paneltest-%d", time.Now().UnixNano())
	d, err := executor.NewDocker(executor.Options{Network: name, LabelPrefix: name})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Ping(ctx); err != nil {
		t.Skipf("no Docker daemon: %v", err)
	}
	st := newStore(t)
	// The panel's token endpoint, as handleRegistryToken serves it.
	var svc *Service
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tokenSrv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		user, pass := Credentials(r)
		tok, err := svc.Token(r.Context(), user, pass, r.Form.Get("service"), r.Form["scope"])
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":%q,"access_token":%q,"expires_in":300}`, tok, tok)
	})}
	go tokenSrv.Serve(ln)
	defer tokenSrv.Close()

	port := freePort(t)
	host := fmt.Sprintf("127.0.0.1:%d", port)
	svc, err = New(Options{
		Executor: d, Store: st, Log: slog.New(slog.DiscardHandler), Name: name + "-registry", Image: "registry:3.0.0",
		Dir: t.TempDir(), DataVolume: name + "-data", Issuer: "berth", PublishPort: port,
		PanelUpstream: "host.docker.internal:1", Realm: "http://" + ln.Addr().String() + TokenPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	image := host + "/shop/web:1"
	t.Cleanup(func() {
		list, _ := d.ListContainers(ctx)
		for _, c := range list {
			_ = d.RemoveContainer(ctx, c.ID, true)
		}
		exec.Command("docker", "logout", host).Run()
		exec.Command("docker", "rmi", image).Run()
		exec.Command("docker", "network", "rm", name).Run()
		exec.Command("docker", "volume", "rm", name+"-data").Run()
		d.Close()
	})
	// The service the registry names is its domain; here, the host clients use.
	st.SetRegistrySettings(ctx, store.RegistrySettings{Enabled: true, Domain: "registry.example.com"})
	if err := svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	waitUp(t, "http://"+host+"/v2/")
	st.CreateRegistryToken(ctx, "ci", store.RegistryPush, "push-secret")
	st.CreateRegistryToken(ctx, "reader", store.RegistryPull, "pull-secret")

	docker := func(stdin string, args ...string) (string, error) {
		cmd := exec.Command("docker", args...)
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := docker("", "pull", "busybox:1.37"); err != nil {
		t.Fatalf("pull busybox: %v\n%s", err, out)
	}
	if out, err := docker("", "tag", "busybox:1.37", image); err != nil {
		t.Fatalf("tag: %v\n%s", err, out)
	}
	if out, err := docker("wrong", "login", host, "-u", "ci", "--password-stdin"); err == nil {
		t.Errorf("login with a wrong token worked:\n%s", out)
	}
	if out, err := docker("pull-secret", "login", host, "-u", "reader", "--password-stdin"); err != nil {
		t.Fatalf("login reader: %v\n%s", err, out)
	}
	if out, err := docker("", "push", image); err == nil {
		t.Errorf("a pull token pushed:\n%s", out)
	}
	if out, err := docker("push-secret", "login", host, "-u", "ci", "--password-stdin"); err != nil {
		t.Fatalf("login ci: %v\n%s", err, out)
	}
	if out, err := docker("", "push", image); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	if tags, err := svc.Tags(ctx, "shop/web"); err != nil || strings.Join(tags, ",") != "1" {
		t.Errorf("tags after push = %v, %v", tags, err)
	}
	// Pulled back with the panel's own account, as a deploy does.
	exec.Command("docker", "logout", host).Run()
	exec.Command("docker", "rmi", image).Run()
	auth := &executor.RegistryAuth{Server: host, Username: svc.SystemUser(), Password: svc.system}
	if err := d.PullImage(ctx, image, auth, nil); err != nil {
		t.Errorf("pull with the panel's account: %v", err)
	}
}
