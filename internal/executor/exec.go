package executor

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// CommandExecutor runs a non-interactive command inside a workload. Secrets belong
// in Env or Input, never command arguments. No host shell is involved.
type CommandExecutor interface {
	Exec(context.Context, string, Command) error
}

// CommandOutputExecutor runs a command like CommandExecutor and returns what it printed on
// standard output. For queries whose result holds no secret: errors still say nothing of it.
type CommandOutputExecutor interface {
	ExecOutput(context.Context, string, Command) (string, error)
}

type Command struct {
	Args  []string
	Env   map[string]string
	Input string
}

// Exec deliberately discards output: SQL errors may echo statements and passwords.
func (d *Docker) Exec(ctx context.Context, cid string, cmd Command) error {
	return d.exec(ctx, cid, cmd, io.Discard)
}

// maxExecOutput bounds what ExecOutput keeps of a command's standard output.
const maxExecOutput = 4 << 20

// ExecOutput is Exec that returns standard output (standard error is still discarded).
func (d *Docker) ExecOutput(ctx context.Context, cid string, cmd Command) (string, error) {
	var out strings.Builder
	if err := d.exec(ctx, cid, cmd, &limitedWriter{w: &out, left: maxExecOutput}); err != nil {
		return "", err
	}
	return out.String(), nil
}

// limitedWriter keeps the first bytes and drops the rest without failing the copy.
type limitedWriter struct {
	w    io.Writer
	left int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if keep := min(len(p), l.left); keep > 0 {
		if _, err := l.w.Write(p[:keep]); err != nil {
			return 0, err
		}
		l.left -= keep
	}
	return len(p), nil
}

func (d *Docker) exec(ctx context.Context, cid string, cmd Command, stdout io.Writer) error {
	env := make([]string, 0, len(cmd.Env))
	for k, v := range cmd.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	created, err := d.cli.ExecCreate(ctx, cid, client.ExecCreateOptions{
		Cmd: cmd.Args, Env: env, AttachStdin: cmd.Input != "", AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return err
	}
	attached, err := d.cli.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer attached.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			attached.Close()
		case <-done:
		}
	}()
	if cmd.Input != "" {
		if _, err := io.Copy(attached.Conn, strings.NewReader(cmd.Input)); err != nil {
			return err
		}
		if err := attached.CloseWrite(); err != nil {
			return err
		}
	}
	// Without a terminal the stream carries both outputs, framed.
	if _, err := stdcopy.StdCopy(stdout, io.Discard, attached.Reader); err != nil {
		return err
	}
	result, err := d.cli.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return err
	}
	if result.Running || result.ExitCode != 0 {
		return fmt.Errorf("database command failed (exit %d)", result.ExitCode)
	}
	return nil
}
