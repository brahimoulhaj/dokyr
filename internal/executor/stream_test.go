package executor

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestCopyExecOutput(t *testing.T) {
	var wire, output bytes.Buffer
	for _, f := range []struct {
		stream byte
		text   string
	}{{1, "SQL dump"}, {2, "private stderr"}, {1, " complete"}} {
		header := make([]byte, 8)
		header[0] = f.stream
		binary.BigEndian.PutUint32(header[4:], uint32(len(f.text)))
		wire.Write(header)
		wire.WriteString(f.text)
	}
	if err := copyExecOutput(&output, &wire); err != nil || output.String() != "SQL dump complete" {
		t.Fatal("stderr leaked or data lost", output.String(), err)
	}
	if err := copyExecOutput(&output, bytes.NewReader([]byte{1, 0, 0})); err == nil {
		t.Fatal("truncated frame accepted")
	}
}
