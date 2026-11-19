package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"panel/internal/brand"
	"panel/internal/store"
)

// NixpacksVersion is the Nixpacks CLI shipped in the panel image; keep it equal to the
// version the Dockerfiles install (TestBuilderPinsMatchImages checks).
const NixpacksVersion = "1.41.0"

// StaticImage serves Static sites: the Caddy release the proxy runs.
const StaticImage = "caddy:2.10.2-alpine"

func (l *Local) nixpacks() string {
	if l.Nixpacks == "" {
		return "nixpacks"
	}
	return l.Nixpacks
}

// ansi matches terminal colour and cursor sequences.
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// buildNixpacks lets Nixpacks detect the language and write a Dockerfile (nothing is
// built by Nixpacks itself), then builds that Dockerfile with buildx like any other, so
// build limits, the build label and secrets work the same way.
//
// Nixpacks declares every build variable as ARG and ENV, so plain build-time variables
// end up in the image's environment, as they do with Nixpacks on its own. Secrets are not
// given to Nixpacks: each RUN step gets them as BuildKit secrets read into its environment
// (RUN --mount=type=secret,id=NAME,env=NAME), never written to a layer.
func (l *Local) buildNixpacks(ctx context.Context, src store.GitSource, dir, tag, appID string, vars Vars, line LineFunc) error {
	contextDir, err := inside(dir, src.BuildContext, true)
	if err != nil {
		return fmt.Errorf("build context %q: %w", displayPath(src.BuildContext), err)
	}
	flags := []string{}
	for _, name := range sortedKeys(vars.Args) {
		flags = append(flags, "--env", name+"="+vars.Args[name])
	}
	if c := src.Options.BuildCommand; c != "" {
		flags = append(flags, "--build-cmd", c)
	}
	// The plan carries the variables Nixpacks adds itself (NODE_ENV, CI…): its Dockerfile
	// declares them as ARG, so the build has to pass their values.
	planOut, err := exec.CommandContext(ctx, l.nixpacks(), append([]string{"plan", contextDir, "--format", "json"}, flags...)...).Output()
	if err != nil {
		return fmt.Errorf("nixpacks plan: %w%s", err, stderrOf(err))
	}
	var plan struct {
		Variables map[string]string `json:"variables"`
	}
	if err := json.Unmarshal(planOut, &plan); err != nil {
		return fmt.Errorf("nixpacks plan: %w", err)
	}
	say(line, "Nixpacks "+NixpacksVersion+": writing the Dockerfile")
	if err := run(ctx, line, os.Environ(), l.nixpacks(), append([]string{"build", contextDir, "--out", contextDir}, flags...)...); err != nil {
		return fmt.Errorf("nixpacks: %w", err)
	}
	dockerfile := filepath.Join(contextDir, ".nixpacks", "Dockerfile")
	if len(vars.Secrets) > 0 {
		raw, err := os.ReadFile(dockerfile)
		if err != nil {
			return fmt.Errorf("nixpacks: %w", err)
		}
		if err := os.WriteFile(dockerfile, []byte(mountSecrets(string(raw), sortedKeys(vars.Secrets))), 0o600); err != nil {
			return err
		}
	}
	builder, release, err := l.ensureBuilder(ctx, line)
	if err != nil {
		return err
	}
	defer release()
	args := append([]string{"buildx", "build"}, builder...)
	args = append(args, "--progress=plain", "--load", "--tag", tag, "--file", dockerfile,
		"--label", brand.Slug+".build="+appID)
	for _, name := range sortedKeys(plan.Variables) {
		args = append(args, "--build-arg", name+"="+plan.Variables[name])
	}
	secrets, cleanup, err := secretFlags(dir, vars.Secrets)
	if err != nil {
		return err
	}
	defer cleanup()
	args = append(append(args, secrets...), contextDir)
	if err := run(ctx, line, os.Environ(), l.docker(), args...); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

// mountSecrets gives every RUN instruction of a Dockerfile the named BuildKit secrets as
// environment variables.
func mountSecrets(dockerfile string, names []string) string {
	if len(names) == 0 {
		return dockerfile
	}
	var mounts strings.Builder
	for _, n := range names {
		fmt.Fprintf(&mounts, "--mount=type=secret,id=%s,env=%s ", n, n)
	}
	lines := strings.Split(dockerfile, "\n")
	for i, l := range lines {
		if rest, ok := strings.CutPrefix(l, "RUN "); ok {
			lines[i] = "RUN " + mounts.String() + rest
		}
	}
	return strings.Join(lines, "\n")
}

// stderrOf returns ": <last line of stderr>" for a failed command, or "".
func stderrOf(err error) string {
	var ee *exec.ExitError
	if !errors.As(err, &ee) || len(ee.Stderr) == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(ee.Stderr)), "\n")
	return ": " + strings.TrimSpace(lines[len(lines)-1])
}

// buildBuildpacks builds with Cloud Native Buildpacks: the builder image's own lifecycle
// (/cnb/lifecycle/creator) runs in a container on the same daemon, detects the language,
// builds and exports the image. This is what the pack CLI does for a trusted builder; pack
// itself is not used, because with build variables it first rewrites the builder image,
// which fails on a daemon with the containerd image store (Docker 29's default).
//
// The source and the build variables are copied into volumes (variables as files under
// /platform/env, plain and secret alike, never on a command line); buildpacks read them as
// the build's environment and do not copy them into the image, though one could. The
// build container gets the build limits. Each app keeps a cache volume between builds.
// The lifecycle takes no label, so the result is re-labelled like Railpack's.
func (l *Local) buildBuildpacks(ctx context.Context, src store.GitSource, dir, tag, appID string, vars Vars, line LineFunc) error {
	contextDir, err := inside(dir, src.BuildContext, true)
	if err != nil {
		return fmt.Errorf("build context %q: %w", displayPath(src.BuildContext), err)
	}
	image := src.Options.Image
	if image == "" {
		image = store.DefaultBuildpacksBuilder
	}
	say(line, "Buildpacks with the builder "+image)
	if exec.CommandContext(ctx, l.docker(), "image", "inspect", image).Run() != nil {
		say(line, "Pulling "+image)
		if err := run(ctx, nil, os.Environ(), l.docker(), "pull", "--quiet", image); err != nil {
			return fmt.Errorf("pull the builder %s: %w", image, err)
		}
	}
	b, err := l.inspectCNBBuilder(ctx, image)
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+"-cnb-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	envDir := filepath.Join(work, "platform", "env")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		return err
	}
	for _, m := range []map[string]string{vars.Args, vars.Secrets} {
		for name, v := range m {
			if !envName.MatchString(name) {
				continue
			}
			if err := os.WriteFile(filepath.Join(envDir, name), []byte(v), 0o600); err != nil {
				return err
			}
		}
	}

	// One name for this build's container and volumes; the cache volume is the app's.
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, brand.Slug+"-cnb-"+strings.ToLower(filepath.Base(work)))
	ws, layers, platform, cache := name+"-ws", name+"-layers", name+"-platform", cnbCacheVolume(appID)
	defer func() {
		// Without the request's context: a cancelled build must still be cleaned up.
		_ = exec.Command(l.docker(), "rm", "--force", name, name+"-prep").Run()
		_ = exec.Command(l.docker(), "volume", "rm", "--force", ws, layers, platform).Run()
	}()
	mounts := []string{"--volume", ws + ":/workspace", "--volume", layers + ":/layers",
		"--volume", platform + ":/platform", "--volume", cache + ":/cache"}

	// A stopped helper container holds the volumes while the files are copied in, then
	// hands them to the builder's user.
	owner := fmt.Sprintf("%d:%d", b.uid, b.gid)
	prep := append([]string{"create", "--name", name + "-prep", "--user", "root", "--entrypoint", "/bin/sh"}, mounts...)
	prep = append(prep, image, "-c", "chown -R "+owner+" /workspace /layers /cache /platform && chmod -R go-rwx /platform")
	if err := run(ctx, nil, os.Environ(), l.docker(), prep...); err != nil {
		return fmt.Errorf("prepare the build: %w", err)
	}
	for _, c := range [][2]string{{contextDir, "/workspace"}, {filepath.Join(work, "platform"), "/platform"}} {
		if err := run(ctx, nil, os.Environ(), l.docker(), "cp", c[0]+string(filepath.Separator)+".", name+"-prep:"+c[1]); err != nil {
			return fmt.Errorf("copy the source into the build: %w", err)
		}
	}
	if err := run(ctx, line, os.Environ(), l.docker(), "start", "--attach", name+"-prep"); err != nil {
		return fmt.Errorf("prepare the build: %w", err)
	}

	raw := tag + "-cnb"
	defer func() { _ = exec.Command(l.docker(), "image", "rm", raw).Run() }()
	// The lifecycle starts as root to reach the Docker socket and export the image, and
	// drops to the builder's user for detect and build.
	args := append([]string{"run", "--rm", "--name", name, "--user", "root",
		"--volume", "/var/run/docker.sock:/var/run/docker.sock",
		"--env", "CNB_PLATFORM_API=" + b.platformAPI}, mounts...)
	lim, err := l.limits(ctx)
	if err != nil {
		return err
	}
	if lim.CPUs > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%g", lim.CPUs))
	}
	if lim.MemoryMB > 0 {
		m := fmt.Sprintf("%dm", lim.MemoryMB)
		args = append(args, "--memory", m, "--memory-swap", m)
	}
	args = append(args, image, "/cnb/lifecycle/creator", "-daemon", "-no-color", "-app", "/workspace", "-layers", "/layers",
		"-cache-dir", "/cache", "-platform", "/platform", "-uid", fmt.Sprint(b.uid), "-gid", fmt.Sprint(b.gid))
	if b.runImage != "" {
		args = append(args, "-run-image", b.runImage)
	}
	args = append(args, raw)
	// Buildpacks colour their output whatever the lifecycle is told; the log is plain text.
	plain := func(s string) { say(line, ansi.ReplaceAllString(s, "")) }
	if err := run(ctx, plain, os.Environ(), l.docker(), args...); err != nil {
		return fmt.Errorf("buildpacks: %s", ansi.ReplaceAllString(err.Error(), ""))
	}
	return l.relabel(ctx, raw, tag, appID, work, line)
}

// cnbCacheVolume names the volume an app's Buildpacks builds keep their cache in.
func cnbCacheVolume(appID string) string {
	return brand.Slug + "-cnb-cache-" + strings.ToLower(appID)
}

// Forget removes what builds keep for an app between deployments (the Buildpacks cache).
func (l *Local) Forget(ctx context.Context, appID string) error {
	vol := cnbCacheVolume(appID)
	if exec.CommandContext(ctx, l.docker(), "volume", "inspect", vol).Run() != nil {
		return nil
	}
	return exec.CommandContext(ctx, l.docker(), "volume", "rm", "--force", vol).Run()
}

type cnbBuilder struct {
	uid, gid    int
	runImage    string
	platformAPI string
}

// maxPlatformAPI is the newest CNB platform API the creator arguments above are written for.
const maxPlatformAPI = "0.13"

// inspectCNBBuilder reads what the lifecycle needs from a builder image: its user, its
// run image and the platform API to speak.
func (l *Local) inspectCNBBuilder(ctx context.Context, image string) (cnbBuilder, error) {
	out, err := exec.CommandContext(ctx, l.docker(), "image", "inspect", "--format",
		`{{json .Config.Env}}|{{index .Config.Labels "io.buildpacks.builder.metadata"}}`, image).Output()
	if err != nil {
		return cnbBuilder{}, fmt.Errorf("inspect the builder %s: %w%s", image, err, stderrOf(err))
	}
	envJSON, metaJSON, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	return parseCNBBuilder(image, envJSON, metaJSON)
}

func parseCNBBuilder(image, envJSON, metaJSON string) (cnbBuilder, error) {
	notBuilder := fmt.Errorf("%s is not a Cloud Native Buildpacks builder image", image)
	var env []string
	var meta struct {
		Stack struct {
			RunImage struct {
				Image string `json:"image"`
			} `json:"runImage"`
		} `json:"stack"`
		Images []struct {
			Image string `json:"image"`
		} `json:"images"`
		Lifecycle struct {
			APIs struct {
				Platform struct {
					Supported []string `json:"supported"`
				} `json:"platform"`
			} `json:"apis"`
		} `json:"lifecycle"`
	}
	if json.Unmarshal([]byte(envJSON), &env) != nil || metaJSON == "" || json.Unmarshal([]byte(metaJSON), &meta) != nil {
		return cnbBuilder{}, notBuilder
	}
	b := cnbBuilder{uid: -1, gid: -1, runImage: meta.Stack.RunImage.Image}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		n, err := strconv.Atoi(v)
		switch {
		case k == "CNB_USER_ID" && err == nil:
			b.uid = n
		case k == "CNB_GROUP_ID" && err == nil:
			b.gid = n
		}
	}
	if b.uid < 0 || b.gid < 0 {
		return cnbBuilder{}, notBuilder
	}
	if b.runImage == "" && len(meta.Images) > 0 {
		b.runImage = meta.Images[0].Image
	}
	// The newest API both sides know.
	for _, v := range meta.Lifecycle.APIs.Platform.Supported {
		if !apiLess(maxPlatformAPI, v) && (b.platformAPI == "" || apiLess(b.platformAPI, v)) {
			b.platformAPI = v
		}
	}
	if b.platformAPI == "" {
		return cnbBuilder{}, fmt.Errorf("the builder %s speaks no platform API up to %s", image, maxPlatformAPI)
	}
	return b, nil
}

// apiLess compares two "major.minor" API versions.
func apiLess(a, b string) bool {
	parse := func(s string) (int, int) {
		major, minor, _ := strings.Cut(s, ".")
		x, _ := strconv.Atoi(major)
		y, _ := strconv.Atoi(minor)
		return x, y
	}
	a1, a2 := parse(a)
	b1, b2 := parse(b)
	return a1 < b1 || (a1 == b1 && a2 < b2)
}

// relabel tags image as tag with the build label added (metadata only, no layer), on the
// daemon's own builder, which sees the image just made.
func (l *Local) relabel(ctx context.Context, image, tag, appID, work string, line LineFunc) error {
	empty := filepath.Join(work, "label")
	if err := os.Mkdir(empty, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(empty, "Dockerfile"), []byte("FROM "+image+"\n"), 0o600); err != nil {
		return err
	}
	if err := run(ctx, line, os.Environ(), l.docker(), "buildx", "build", "--progress=plain", "--load",
		"--tag", tag, "--label", brand.Slug+".build="+appID, empty); err != nil {
		return fmt.Errorf("label image: %w", err)
	}
	return nil
}

// buildStatic serves a folder of the repository with Caddy. With a build command, the
// command runs first in the build image (Node by default) at the build context, and the
// output folder is taken from there; without one, the folder is served as it is in the
// repository. The site answers on $PORT, gzip and zstd compressed, and a single-page app
// gets index.html for paths that are not files. Build variables reach the build command
// only (plain ones as build arguments, secrets as BuildKit secrets): nothing of the build
// stage is in the served image but the output folder.
func (l *Local) buildStatic(ctx context.Context, src store.GitSource, dir, tag, appID string, vars Vars, line LineFunc) error {
	contextDir, err := inside(dir, src.BuildContext, true)
	if err != nil {
		return fmt.Errorf("build context %q: %w", displayPath(src.BuildContext), err)
	}
	o := src.Options
	if o.BuildCommand == "" {
		if _, err := inside(contextDir, o.OutputDir, true); err != nil {
			return fmt.Errorf("output folder %q in the build context: %w", displayPath(o.OutputDir), err)
		}
	}
	work, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+"-static-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	dockerfile := filepath.Join(work, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte(staticDockerfile(o, sortedKeys(vars.Args), sortedKeys(vars.Secrets))), 0o600); err != nil {
		return err
	}
	if o.BuildCommand != "" {
		image := o.Image
		if image == "" {
			image = store.DefaultStaticBuildImage
		}
		say(line, fmt.Sprintf("Static site: running the build command in %s, then serving %s with Caddy", image, displayPath(o.OutputDir)))
	} else {
		say(line, fmt.Sprintf("Static site: serving %s with Caddy", displayPath(o.OutputDir)))
	}
	builder, release, err := l.ensureBuilder(ctx, line)
	if err != nil {
		return err
	}
	defer release()
	args := append([]string{"buildx", "build"}, builder...)
	args = append(args, "--progress=plain", "--load", "--tag", tag, "--file", dockerfile,
		"--label", brand.Slug+".build="+appID)
	for _, name := range sortedKeys(vars.Args) {
		args = append(args, "--build-arg", name+"="+vars.Args[name])
	}
	secrets, cleanup, err := secretFlags(dir, vars.Secrets)
	if err != nil {
		return err
	}
	defer cleanup()
	args = append(append(args, secrets...), contextDir)
	if err := run(ctx, line, os.Environ(), l.docker(), args...); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

// staticDockerfile is the Dockerfile of a Static site. A first stage holds the files
// (built, or as they are), without the repository's .git; only the output folder reaches
// the served image. Names are environment variable names; values never appear in it.
func staticDockerfile(o store.BuildOptions, args, secrets []string) string {
	var b strings.Builder
	out := strings.TrimSuffix(path.Clean("/"+o.OutputDir), "/") // "" for the root
	image := StaticImage
	if o.BuildCommand != "" {
		image = o.Image
		if image == "" {
			image = store.DefaultStaticBuildImage
		}
	}
	fmt.Fprintf(&b, "FROM %s AS site\nWORKDIR /src\n", image)
	if o.BuildCommand != "" {
		for _, n := range args {
			if envName.MatchString(n) {
				fmt.Fprintf(&b, "ARG %s\n", n)
			}
		}
	}
	// .git is dropped in the same step that copies it, so no layer keeps it.
	b.WriteString("RUN --mount=type=bind,target=/repo cp -a /repo/. /src/ && rm -rf /src/.git\n")
	if o.BuildCommand != "" {
		b.WriteString("RUN ")
		for _, n := range secrets {
			if envName.MatchString(n) {
				fmt.Fprintf(&b, "--mount=type=secret,id=%s,env=%s ", n, n)
			}
		}
		// JSON form: the command runs in sh -c exactly as written, newlines included.
		cmd, _ := json.Marshal([]string{"/bin/sh", "-c", o.BuildCommand})
		fmt.Fprintf(&b, "%s\n", cmd)
	}
	fmt.Fprintf(&b, "\nFROM %s\n", StaticImage)
	fmt.Fprintf(&b, "COPY --from=site /src%s/ /srv/\n", out)
	b.WriteString("COPY <<\"CADDYFILE\" /etc/caddy/Caddyfile\n")
	b.WriteString(staticCaddyfile(o.SPA))
	b.WriteString("CADDYFILE\n")
	return b.String()
}

func staticCaddyfile(spa bool) string {
	fallback := ""
	if spa {
		fallback = "\ttry_files {path} {path}/ /index.html\n"
	}
	return "{\n\tadmin off\n\tauto_https off\n\tpersist_config off\n}\n\n" +
		":{$PORT:80} {\n\troot * /srv\n\tencode zstd gzip\n" + fallback + "\tfile_server {\n\t\thide .git\n\t}\n}\n"
}

// PHPExtensionsVariable is the build variable Railpack reads the PHP extensions to install
// from, beyond those composer.json requires.
const PHPExtensionsVariable = "RAILPACK_PHP_EXTENSIONS"

// DefaultPHPExtensions are the database drivers a PHP image built by Railpack would
// otherwise lack: Railpack adds one only when it sees DB_CONNECTION at build time or an
// ext-pdo_* requirement in composer.json, and a Laravel project has neither by default.
const DefaultPHPExtensions = "pdo_mysql,pdo_pgsql"

// RailpackPHP reports whether the fetched workspace will be built by Railpack as a PHP
// project: the builder is Railpack (chosen, or Automatic without a Dockerfile) and the
// build context has a composer.json or an index.php, which is how Railpack detects PHP.
func RailpackPHP(dir string, src store.GitSource) bool {
	switch src.Builder {
	case store.BuilderRailpack:
	case store.BuilderAuto:
		if hasDockerfile(dir, src) {
			return false
		}
	default:
		return false
	}
	contextDir, err := inside(dir, src.BuildContext, true)
	if err != nil {
		return false
	}
	for _, f := range []string{"composer.json", "index.php"} {
		if _, err := inside(contextDir, f, false); err == nil {
			return true
		}
	}
	return false
}
