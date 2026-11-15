package executor

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/moby/moby/client"
)

// CommandExecutor runs a non-interactive command inside a workload. Secrets belong
// in Env or Input, never command arguments. No host shell is involved.
type CommandExecutor interface {
	Exec(context.Context, string, Command) error
}

type Command struct {
	Args  []string
	Env   map[string]string
	Input string
}

// Exec deliberately discards output: SQL errors may echo statements and passwords.
func (d *Docker) Exec(ctx context.Context, cid string, cmd Command) error {
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
	if _, err := io.Copy(io.Discard, attached.Reader); err != nil {
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
