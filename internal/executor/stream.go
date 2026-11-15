package executor

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sort"

	"github.com/moby/moby/client"
)

// StreamExecutor streams SQL dumps without retaining them in process memory.
// Stderr is always discarded: database diagnostics can contain row data or secrets.
type StreamExecutor interface {
	ExecStream(context.Context, string, Command, io.Reader, io.Writer) error
}

func (d *Docker) ExecStream(ctx context.Context, cid string, cmd Command, input io.Reader, output io.Writer) error {
	env := []string{}
	for k, v := range cmd.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	created, err := d.cli.ExecCreate(ctx, cid, client.ExecCreateOptions{Cmd: cmd.Args, Env: env, AttachStdin: input != nil, AttachStdout: true, AttachStderr: true})
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
	sent := make(chan error, 1)
	if input != nil {
		go func() {
			_, e := io.Copy(attached.Conn, input)
			closeErr := attached.CloseWrite()
			if e == nil {
				e = closeErr
			} else {
				attached.Close()
			}
			sent <- e
		}()
	} else {
		sent <- nil
	}
	if output == nil {
		output = io.Discard
	}
	if err := copyExecOutput(output, attached.Reader); err != nil {
		return err
	}
	if err := <-sent; err != nil {
		return err
	}
	result, err := d.cli.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return err
	}
	if result.Running || result.ExitCode != 0 {
		return fmt.Errorf("database stream failed (exit %d)", result.ExitCode)
	}
	return nil
}

func copyExecOutput(out io.Writer, in io.Reader) error {
	var header [8]byte
	for {
		_, err := io.ReadFull(in, header[:])
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		dest := io.Discard
		if header[0] == 1 {
			dest = out
		}
		if _, err = io.CopyN(dest, in, int64(binary.BigEndian.Uint32(header[4:]))); err != nil {
			return err
		}
	}
}
