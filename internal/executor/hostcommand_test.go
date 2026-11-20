package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The host script, run by the local sh as the current user (no HOST_USER, so no su).
func runHostScript(t *testing.T, tmp, command string) (string, int) {
	t.Helper()
	cmd := exec.Command("sh", "-c", hostCommand)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + tmp, "TMPDIR=" + tmp, "HOST_USER=", "HOST_CMD=" + command}
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	return string(out), code
}

func TestHostCommandShebang(t *testing.T) {
	if _, err := exec.LookPath("awk"); err != nil {
		t.Skip("no awk")
	}
	tmp := t.TempDir()
	// Without a shebang the script is a shell script, as before.
	if out, code := runHostScript(t, tmp, "echo plain; exit 3"); out != "plain\n" || code != 3 {
		t.Fatalf("plain script: %q exit %d", out, code)
	}
	// With one, the named interpreter runs it: this is not valid shell.
	awk := "#!/usr/bin/env awk -f\r\nBEGIN { printf \"%s|%d\\n\", \"it's $HOME\", 6 * 7; exit 4 }"
	if out, code := runHostScript(t, tmp, awk); out != "it's $HOME|42\n" || code != 4 {
		t.Fatalf("awk script: %q exit %d", out, code)
	}
	if out, code := runHostScript(t, tmp, "#!/bin/sh\nprintf '%s' \"$0\" | grep -c host-job"); strings.TrimSpace(out) != "1" || code != 0 {
		t.Fatalf("sh script: %q exit %d", out, code)
	}
	if _, code := runHostScript(t, tmp, "#!/no/such/interpreter\necho no"); code == 0 {
		t.Fatal("a missing interpreter must fail the run")
	}
	// The temporary script never outlives its run.
	if left, _ := filepath.Glob(filepath.Join(tmp, ".host-job.*")); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}
