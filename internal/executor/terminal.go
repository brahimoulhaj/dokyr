package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Terminal is an attached PTY: a shell in an app container, or (OpenHostTerminal) on the host.
type Terminal interface {
	io.ReadWriteCloser
	Resize(context.Context, uint, uint) error
}

type TerminalExecutor interface {
	OpenTerminal(context.Context, string, string, uint, uint) (Terminal, error)
}

// ShellAuto picks the most comfortable shell the container has. Plain sh is often dash
// (Debian, Ubuntu), which has no completion and no history; bash and BusyBox ash have both.
const ShellAuto = "auto"

// ValidShell reports whether shell is one OpenTerminal runs.
func ValidShell(shell string) bool {
	return shell == ShellAuto || shell == "/bin/sh" || shell == "/bin/bash" || shell == "/bin/ash"
}

func shellCommand(shell string) []string {
	if shell == ShellAuto {
		return []string{"/bin/sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash -i; command -v ash >/dev/null 2>&1 && exec ash -i; exec sh -i"}
	}
	return []string{shell, "-i"}
}

func (d *Docker) OpenTerminal(ctx context.Context, containerID, shell string, cols, rows uint) (Terminal, error) {
	if !ValidShell(shell) {
		return nil, errors.New("unsupported shell")
	}
	created, err := d.cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		Cmd: shellCommand(shell), TTY: true, AttachStdin: true, AttachStdout: true, AttachStderr: true,
		Env: []string{"TERM=xterm-256color"}, ConsoleSize: client.ConsoleSize{Width: cols, Height: rows},
	})
	if err != nil {
		return nil, err
	}
	attached, err := d.cli.ExecAttach(ctx, created.ID, client.ExecAttachOptions{TTY: true, ConsoleSize: client.ConsoleSize{Width: cols, Height: rows}})
	if err != nil {
		return nil, err
	}
	return &dockerTerminal{docker: d, id: created.ID, attached: attached}, nil
}

type dockerTerminal struct {
	docker   *Docker
	id       string
	attached client.ExecAttachResult
	once     sync.Once
}

func (t *dockerTerminal) Read(p []byte) (int, error) { return t.attached.Reader.Read(p) }
func (t *dockerTerminal) Write(p []byte) (int, error) {
	t.attached.Conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return t.attached.Conn.Write(p)
}
func (t *dockerTerminal) Close() error { t.once.Do(func() { t.attached.Close() }); return nil }
func (t *dockerTerminal) Resize(ctx context.Context, cols, rows uint) error {
	_, err := t.docker.cli.ExecResize(ctx, t.id, client.ExecResizeOptions{Width: cols, Height: rows})
	return err
}

// HostTerminalExecutor opens a shell on the Docker host itself.
type HostTerminalExecutor interface {
	OpenHostTerminal(ctx context.Context, image, user string, cols, rows uint) (Terminal, error)
}

var hostUser = regexp.MustCompile(`^[a-z_][a-z0-9_.-]{0,31}$`)

// ValidHostUser reports whether name can be a user of the host; empty stands for root.
func ValidHostUser(name string) bool { return name == "" || hostUser.MatchString(name) }

// hostShell runs after nsenter, so on the host. With HOST_USER set it becomes that user's
// login shell; otherwise root's, bash when the host has it.
const hostShell = `if [ -n "$HOST_USER" ]; then u=$HOST_USER; unset HOST_USER
id -u "$u" >/dev/null 2>&1 || { echo "User $u does not exist on this server. Create it, or choose another in Settings > Security."; exit 1; }
exec su - "$u"; fi
cd "$HOME" 2>/dev/null || cd /; command -v bash >/dev/null 2>&1 && exec bash -l; exec sh -l`

// OpenHostTerminal starts a short-lived privileged container in the host's PID namespace
// and enters every namespace of PID 1, which is a root shell on the host; a user other
// than root is reached from there with su. The image only needs nsenter (BusyBox has it);
// the container is removed when the session ends.
func (d *Docker) OpenHostTerminal(ctx context.Context, image, user string, cols, rows uint) (Terminal, error) {
	if !ValidHostUser(user) {
		return nil, fmt.Errorf("invalid host user %q", user)
	}
	if user == "root" {
		user = ""
	}
	created, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      image,
			Entrypoint: []string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--"},
			Cmd:        []string{"sh", "-c", hostShell},
			Env: []string{
				"TERM=xterm-256color", "HOME=/root",
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"HOST_USER=" + user,
			},
			Tty: true, OpenStdin: true, AttachStdin: true, AttachStdout: true, AttachStderr: true,
			// Not labelled as managed: deployment and metrics code must not see it as a workload.
			Labels: map[string]string{d.prefix + ".host-terminal": "true"},
		},
		HostConfig: &container.HostConfig{
			Privileged:  true,
			PidMode:     "host",
			NetworkMode: "none", // nsenter joins the host's network namespace anyway
			AutoRemove:  true,
			ConsoleSize: [2]uint{rows, cols},
		},
	})
	if err != nil {
		return nil, err
	}
	remove := func() {
		rmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, _ = d.cli.ContainerRemove(rmCtx, created.ID, client.ContainerRemoveOptions{Force: true})
	}
	attached, err := d.cli.ContainerAttach(ctx, created.ID, client.ContainerAttachOptions{Stream: true, Stdin: true, Stdout: true, Stderr: true})
	if err != nil {
		remove()
		return nil, err
	}
	if _, err := d.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		attached.Close()
		remove()
		return nil, err
	}
	return &hostTerminal{docker: d, id: created.ID, attached: attached, remove: remove}, nil
}

type hostTerminal struct {
	docker   *Docker
	id       string
	attached client.ContainerAttachResult
	remove   func()
	once     sync.Once
}

func (t *hostTerminal) Read(p []byte) (int, error) { return t.attached.Reader.Read(p) }
func (t *hostTerminal) Write(p []byte) (int, error) {
	t.attached.Conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return t.attached.Conn.Write(p)
}

// Close also kills the shell: closing the attach stream alone would leave it running.
func (t *hostTerminal) Close() error {
	t.once.Do(func() { t.attached.Close(); t.remove() })
	return nil
}
func (t *hostTerminal) Resize(ctx context.Context, cols, rows uint) error {
	_, err := t.docker.cli.ContainerResize(ctx, t.id, client.ContainerResizeOptions{Width: cols, Height: rows})
	return err
}
