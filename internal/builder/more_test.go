package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/store"
)

func TestMountSecrets(t *testing.T) {
	in := "FROM x\nRUN npm i\nRUN --mount=type=cache,target=/c make\nENV A=1\n"
	got := mountSecrets(in, []string{"API_KEY", "TOKEN"})
	want := "FROM x\nRUN --mount=type=secret,id=API_KEY,env=API_KEY --mount=type=secret,id=TOKEN,env=TOKEN npm i\n" +
		"RUN --mount=type=secret,id=API_KEY,env=API_KEY --mount=type=secret,id=TOKEN,env=TOKEN --mount=type=cache,target=/c make\nENV A=1\n"
	if got != want {
		t.Fatalf("got\n%s", got)
	}
	if mountSecrets(in, nil) != in {
		t.Fatal("changed without secrets")
	}
}

func TestStaticDockerfile(t *testing.T) {
	plain := staticDockerfile(store.BuildOptions{OutputDir: "public"}, nil, nil)
	for _, want := range []string{"FROM " + StaticImage + " AS site", "rm -rf /src/.git", "COPY --from=site /src/public/ /srv/", ":{$PORT:80}", "file_server"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain site misses %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "try_files") || strings.Contains(plain, "/bin/sh") {
		t.Fatalf("plain site has a fallback or a build step:\n%s", plain)
	}
	built := staticDockerfile(store.BuildOptions{BuildCommand: "npm ci && npm run build", OutputDir: "dist", SPA: true},
		[]string{"VITE_API"}, []string{"NPM_TOKEN"})
	for _, want := range []string{"FROM node:22-alpine AS site", "ARG VITE_API", "--mount=type=secret,id=NPM_TOKEN,env=NPM_TOKEN",
		`["/bin/sh","-c","npm ci \u0026\u0026 npm run build"]`, "COPY --from=site /src/dist/ /srv/", "try_files {path} {path}/ /index.html"} {
		if !strings.Contains(built, want) {
			t.Fatalf("built site misses %q:\n%s", want, built)
		}
	}
	if root := staticDockerfile(store.BuildOptions{}, nil, nil); !strings.Contains(root, "COPY --from=site /src/ /srv/") {
		t.Fatalf("root output:\n%s", root)
	}
}

// TestBuildStatic builds two Static sites on a real daemon and asks them for pages.
func TestBuildStatic(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a Docker daemon")
	}
	if err := exec.Command("docker", "version").Run(); err != nil {
		t.Skip("no Docker daemon")
	}
	ctx := context.Background()
	b := &Local{}
	serve := func(tag string) func(path string) string {
		name := fmt.Sprintf("panel-static-test-%d", time.Now().UnixNano())
		if out, err := exec.Command("docker", "run", "-d", "--rm", "--name", name, "-e", "PORT=8080", tag).CombinedOutput(); err != nil {
			t.Fatalf("run: %s", out)
		}
		t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
		return func(path string) string {
			for i := 0; i < 50; i++ {
				out, err := exec.Command("docker", "exec", name, "wget", "-qO-", "http://127.0.0.1:8080"+path).CombinedOutput()
				if err == nil {
					return string(out)
				}
				if strings.Contains(string(out), "404") {
					return "404"
				}
				time.Sleep(100 * time.Millisecond)
			}
			return "unreachable"
		}
	}

	// As it is: the repository's root, without its .git.
	plain := filepath.Join(t.TempDir(), "plain")
	write(t, filepath.Join(plain, "index.html"), "home\n")
	write(t, filepath.Join(plain, ".git", "config"), "secret\n")
	tag := "panel-builder-test:static-plain"
	t.Cleanup(func() { exec.Command("docker", "image", "rm", "-f", tag).Run() })
	src := store.GitSource{Builder: store.BuilderStatic}
	var lines []string
	if err := b.Build(ctx, src, plain, tag, "app1", Vars{}, func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatalf("build: %v\n%s", err, strings.Join(lines, "\n"))
	}
	get := serve(tag)
	if got := get("/"); got != "home\n" {
		t.Fatalf("/ = %q", got)
	}
	if got := get("/.git/config"); got != "404" {
		t.Fatalf(".git served: %q", got)
	}
	if got := get("/nowhere"); got != "404" {
		t.Fatalf("unknown path without SPA = %q", got)
	}
	label, _ := exec.Command("docker", "image", "inspect", "-f", `{{index .Config.Labels "dokyr.build"}}`, tag).Output()
	if strings.TrimSpace(string(label)) != "app1" {
		t.Fatalf("label = %q", label)
	}

	// Built: the command runs with a build variable and a secret, output served as an SPA.
	app := filepath.Join(t.TempDir(), "app")
	write(t, filepath.Join(app, "README"), "source\n")
	tag2 := "panel-builder-test:static-built"
	t.Cleanup(func() { exec.Command("docker", "image", "rm", "-f", tag2).Run() })
	src = store.GitSource{Builder: store.BuilderStatic, Options: store.BuildOptions{
		Image: StaticImage, OutputDir: "out", SPA: true,
		BuildCommand: "mkdir -p out && test \"$TOKEN\" = s3cr3t-value && echo \"app $GREETING\" > out/index.html",
	}}
	vars := Vars{Args: map[string]string{"GREETING": "hello"}, Secrets: map[string]string{"TOKEN": "s3cr3t-value"}}
	lines = nil
	if err := b.Build(ctx, src, app, tag2, "app2", vars, func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatalf("build: %v\n%s", err, strings.Join(lines, "\n"))
	}
	get = serve(tag2)
	if got := get("/"); got != "app hello\n" {
		t.Fatalf("/ = %q", got)
	}
	if got := get("/deep/route"); got != "app hello\n" {
		t.Fatalf("SPA fallback = %q", got)
	}
	if files, err := exec.Command("docker", "run", "--rm", "--entrypoint", "ls", tag2, "-A", "/srv").Output(); err != nil || strings.TrimSpace(string(files)) != "index.html" {
		t.Fatalf("served folder = %q, %v (only the output folder belongs there)", files, err)
	}
	history, _ := exec.Command("docker", "image", "history", "--no-trunc", tag2).CombinedOutput()
	inspect, _ := exec.Command("docker", "image", "inspect", tag2).CombinedOutput()
	if strings.Contains(string(history)+string(inspect), "s3cr3t-value") {
		t.Fatal("the secret is in the image")
	}

	// A missing output folder without a build command fails before building.
	if err := b.Build(ctx, store.GitSource{Builder: store.BuilderStatic, Options: store.BuildOptions{OutputDir: "dist"}}, plain, tag, "app1", Vars{}, nil); err == nil || !strings.Contains(err.Error(), "output folder") {
		t.Fatalf("missing output: %v", err)
	}
}

func lookTool(t *testing.T, env, name string) string {
	t.Helper()
	if p := os.Getenv(env); p != "" {
		return p
	}
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not installed; set %s", name, env)
	}
	return p
}

// TestBuildNixpacks builds a Node app with Nixpacks on a real daemon: a plain build
// variable reaches the build, a secret reaches the RUN steps and stays out of the image.
func TestBuildNixpacks(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a Docker daemon and network")
	}
	np := lookTool(t, "DOKYR_TEST_NIXPACKS", "nixpacks")
	dir := filepath.Join(t.TempDir(), "np")
	write(t, filepath.Join(dir, "package.json"), `{"name":"t","version":"1.0.0","scripts":{"build":"node build.js","start":"node index.js"}}`)
	write(t, filepath.Join(dir, "build.js"), `if (process.env.TOKEN !== "s3cr3t-value") { console.error("no secret"); process.exit(1) }
require("fs").writeFileSync("built.txt", "built " + process.env.GREETING)`+"\n")
	write(t, filepath.Join(dir, "index.js"), `require("http").createServer((q,r)=>r.end("ok")).listen(process.env.PORT||3000)`+"\n")
	tag := "panel-builder-test:nixpacks"
	t.Cleanup(func() { exec.Command("docker", "image", "rm", "-f", tag).Run() })
	var lines []string
	b := &Local{Nixpacks: np}
	src := store.GitSource{Builder: store.BuilderNixpacks}
	vars := Vars{Args: map[string]string{"GREETING": "hello"}, Secrets: map[string]string{"TOKEN": "s3cr3t-value"}}
	if err := b.Build(context.Background(), src, dir, tag, "app1", vars, func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatalf("build: %v\n%s", err, strings.Join(lines, "\n"))
	}
	out, err := exec.Command("docker", "run", "--rm", "--entrypoint", "cat", tag, "/app/built.txt").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "built hello" {
		t.Fatalf("built.txt = %q, %v", out, err)
	}
	label, _ := exec.Command("docker", "image", "inspect", "-f", `{{index .Config.Labels "dokyr.build"}}`, tag).Output()
	if strings.TrimSpace(string(label)) != "app1" {
		t.Fatalf("label = %q", label)
	}
	inspect, _ := exec.Command("docker", "image", "inspect", tag).CombinedOutput()
	history, _ := exec.Command("docker", "image", "history", "--no-trunc", tag).CombinedOutput()
	if strings.Contains(string(inspect)+string(history), "s3cr3t-value") {
		t.Fatal("the secret is in the image")
	}
}

func TestParseCNBBuilder(t *testing.T) {
	env := `["PATH=/bin","CNB_USER_ID=1000","CNB_GROUP_ID=1001","CNB_STACK_ID=x"]`
	meta := `{"stack":{"runImage":{"image":"docker.io/heroku/heroku:24"}},"lifecycle":{"apis":{"platform":{"supported":["0.7","0.12","0.13","0.15"]}}}}`
	b, err := parseCNBBuilder("b", env, meta)
	if err != nil || b.uid != 1000 || b.gid != 1001 || b.runImage != "docker.io/heroku/heroku:24" || b.platformAPI != "0.13" {
		t.Fatalf("builder = %+v, %v", b, err)
	}
	// The run image may only be in the newer list; an older lifecycle picks its newest API.
	b, err = parseCNBBuilder("b", env, `{"images":[{"image":"run:1"}],"lifecycle":{"apis":{"platform":{"supported":["0.9","0.10"]}}}}`)
	if err != nil || b.runImage != "run:1" || b.platformAPI != "0.10" {
		t.Fatalf("builder = %+v, %v", b, err)
	}
	for _, bad := range [][2]string{{env, ""}, {`["PATH=/bin"]`, meta}, {env, `{"lifecycle":{"apis":{"platform":{"supported":["0.99"]}}}}`}, {"null", "null"}} {
		if _, err := parseCNBBuilder("nginx", bad[0], bad[1]); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

// TestBuildBuildpacks builds a Node app with the default builder on a real daemon, under
// build limits: a build variable and a multi-line secret reach the build, the secret is
// not in the image, and only the app's cache volume is left behind.
func TestBuildBuildpacks(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a Docker daemon and network")
	}
	if err := exec.Command("docker", "image", "inspect", store.DefaultBuildpacksBuilder).Run(); err != nil {
		t.Skipf("no %s image locally (1.5 GB); pull it to run this test", store.DefaultBuildpacksBuilder)
	}
	dir := filepath.Join(t.TempDir(), "bp")
	write(t, filepath.Join(dir, "package.json"), `{"name":"t","version":"1.0.0","engines":{"node":"22.x"},"scripts":{"build":"node build.js","start":"node index.js"}}`)
	write(t, filepath.Join(dir, "package-lock.json"), `{"name":"t","version":"1.0.0","lockfileVersion":3,"requires":true,"packages":{"":{"name":"t","version":"1.0.0","engines":{"node":"22.x"}}}}`)
	write(t, filepath.Join(dir, "build.js"), `if (process.env.TOKEN !== "s3cr3t\nvalue") { console.error("no secret"); process.exit(1) }
require("fs").writeFileSync("built.txt", "built " + process.env.GREETING)`+"\n")
	write(t, filepath.Join(dir, "index.js"), `require("http").createServer((q,r)=>r.end("ok")).listen(process.env.PORT||3000)`+"\n")
	tag := "panel-builder-test:buildpacks"
	appID := "01TESTAPPCNB"
	b := &Local{Limits: Limits{CPUs: 2, MemoryMB: 2048}}
	ctx := context.Background()
	t.Cleanup(func() {
		exec.Command("docker", "image", "rm", "-f", tag).Run()
		b.Forget(ctx, appID)
	})
	var lines []string
	src := store.GitSource{Builder: store.BuilderBuildpacks}
	vars := Vars{Args: map[string]string{"GREETING": "hi"}, Secrets: map[string]string{"TOKEN": "s3cr3t\nvalue"}}
	if err := b.Build(ctx, src, dir, tag, appID, vars, func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatalf("build: %v\n%s", err, strings.Join(lines, "\n"))
	}
	if strings.Contains(strings.Join(lines, "\n"), "\x1b") {
		t.Error("colour codes in the build log")
	}
	label, _ := exec.Command("docker", "image", "inspect", "-f", `{{index .Config.Labels "dokyr.build"}}`, tag).Output()
	if strings.TrimSpace(string(label)) != appID {
		t.Fatalf("label = %q", label)
	}
	out, err := exec.Command("docker", "run", "--rm", "--entrypoint", "cat", tag, "/workspace/built.txt").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "built hi" {
		t.Fatalf("built.txt = %q, %v", out, err)
	}
	inspect, _ := exec.Command("docker", "image", "inspect", tag).CombinedOutput()
	if strings.Contains(string(inspect), "s3cr3t") {
		t.Fatal("the secret is in the image")
	}
	if err := exec.Command("docker", "image", "inspect", tag+"-cnb").Run(); err == nil {
		t.Error("the unlabelled image is still tagged")
	}
	vols, _ := exec.Command("docker", "volume", "ls", "--format", "{{.Name}}", "--filter", "name=dokyr-cnb-").Output()
	// Other apps' caches may be there; this build's own volumes must be gone and its cache kept.
	kept := false
	for _, v := range strings.Fields(string(vols)) {
		if v == cnbCacheVolume(appID) {
			kept = true
		} else if !strings.HasPrefix(v, "dokyr-cnb-cache-") {
			t.Errorf("build volume left behind: %s", v)
		}
	}
	if !kept {
		t.Error("the app's cache volume was not kept")
	}
	if err := b.Forget(ctx, appID); err != nil {
		t.Fatal(err)
	}
	if exec.Command("docker", "volume", "inspect", cnbCacheVolume(appID)).Run() == nil {
		t.Error("cache volume not removed")
	}
	// An image that is not a builder is refused with a clear message.
	bad := store.GitSource{Builder: store.BuilderBuildpacks, Options: store.BuildOptions{Image: StaticImage}}
	if err := b.Build(ctx, bad, dir, tag, appID, Vars{}, nil); err == nil || !strings.Contains(err.Error(), "not a Cloud Native Buildpacks builder") {
		t.Errorf("non-builder image: %v", err)
	}
}

func TestNoSymlinks(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, ".nixpacks")
	if err := noSymlinks(out); err != nil {
		t.Fatalf("a missing folder: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(out, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "Dockerfile"), []byte("FROM scratch"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := noSymlinks(out); err != nil {
		t.Fatalf("plain files: %v", err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(out, "assets", "x")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := noSymlinks(out); err == nil {
		t.Fatal("accepted a symbolic link inside the folder")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if err := noSymlinks(link); err == nil {
		t.Fatal("accepted a folder that is a symbolic link")
	}
}

func TestProcessVariable(t *testing.T) {
	for _, name := range []string{"PATH", "LD_PRELOAD", "DOCKER_HOST", "GIT_SSH_COMMAND", "MISE_SHIMS_DIR", "HOME", "https_proxy"} {
		if !processVariable(name) {
			t.Errorf("%s should be refused as a secret build variable", name)
		}
	}
	for _, name := range []string{"NPM_TOKEN", "DATABASE_URL", "RAILPACK_NODE_VERSION", "PATHS", "MY_PATH", "LDAP_PASSWORD"} {
		if processVariable(name) {
			t.Errorf("%s is an ordinary name", name)
		}
	}
}
