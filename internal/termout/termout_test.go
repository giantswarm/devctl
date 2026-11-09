package termout_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/termout"
)

func TestPrefixesNonTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var buffer bytes.Buffer
	file, err := os.CreateTemp(t.TempDir(), "diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	reader, pipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = pipe.Close() })

	for _, tc := range []struct {
		name   string
		writer io.Writer
	}{
		{"buffer", &buffer},
		{"file", file},
		{"pipe", pipe},
		{"discard", io.Discard},
		{"custom writer", noWrites{t}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := termout.Error(tc.writer); got != "Error" {
				t.Errorf("Error = %q, want plain Error", got)
			}
			if got := termout.Warning(tc.writer); got != "Warning" {
				t.Errorf("Warning = %q, want plain Warning", got)
			}
		})
	}
	if buffer.Len() != 0 {
		t.Errorf("formatting wrote to the buffer: %q", buffer.String())
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("formatting closed the caller's file: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("formatting wrote %d bytes to the file", info.Size())
	}
	if _, err := pipe.Stat(); err != nil {
		t.Fatalf("formatting closed the caller's pipe: %v", err)
	}
}

type noWrites struct{ t *testing.T }

func (w noWrites) Write([]byte) (int, error) {
	w.t.Fatal("formatting must not write to the supplied writer")
	return 0, nil
}
