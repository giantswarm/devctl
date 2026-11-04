package version

import (
	"errors"
	"io"
	"testing"

	"github.com/spf13/cobra"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestRunReturnsOutputError(t *testing.T) {
	r := &runner{flag: &flag{}, stdout: failingWriter{}}
	if err := r.Run(&cobra.Command{}, nil); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Run: %v, want the output error", err)
	}
}
