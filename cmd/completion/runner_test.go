package completion

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

var shellMarkers = map[string]string{
	"bash":       "_fixturectl_inspect()",
	"zsh":        "#compdef fixturectl",
	"fish":       "complete -c fixturectl",
	"powershell": "Register-ArgumentCompleter -CommandName 'fixturectl'",
}

func completionRoot(t *testing.T, stdout, stderr io.Writer) *cobra.Command {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := New(Config{Logger: logger, Stdout: stdout, Stderr: stderr})
	if err != nil {
		t.Fatal(err)
	}
	root := &cobra.Command{Use: "fixturectl", SilenceErrors: true, SilenceUsage: true}
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	inspect := &cobra.Command{Use: "inspect", Run: func(*cobra.Command, []string) {}}
	inspect.Flags().String("fixture-flag", "", "A fixture flag.")
	root.AddCommand(completion, inspect)
	return root
}

// captureStdout detects output that bypasses Config.Stdout. These tests must
// remain sequential because os.Stdout is shared by the process.
func captureStdout(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = f
	t.Cleanup(func() {
		os.Stdout = previous
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

func TestCompletionOutput(t *testing.T) {
	for shell, marker := range shellMarkers {
		for _, useDefault := range []bool{false, true} {
			name := shell + "/configured"
			if useDefault {
				name = shell + "/default"
			}
			t.Run(name, func(t *testing.T) {
				processOutput := captureStdout(t)
				var stdout, stderr bytes.Buffer
				var output io.Writer = &stdout
				if useDefault {
					output = nil
				}
				root := completionRoot(t, output, &stderr)
				root.SetArgs([]string{"completion", shell})
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				processBytes, err := os.ReadFile(processOutput.Name())
				if err != nil {
					t.Fatal(err)
				}
				script := stdout.String()
				if useDefault {
					script = string(processBytes)
				} else if len(processBytes) != 0 {
					t.Errorf("wrote %d script bytes to process stdout instead of Config.Stdout", len(processBytes))
				}
				if !strings.Contains(script, marker) {
					t.Errorf("output lacks %q: no completion script for the parent command", marker)
				}
				if shell == "bash" && !strings.Contains(script, "--fixture-flag") {
					t.Error("bash script omits the sibling command's flag")
				}
				if stderr.Len() != 0 {
					t.Errorf("script generation wrote %d bytes to stderr", stderr.Len())
				}
			})
		}
	}
}

type completionWriterFunc func([]byte) (int, error)

func (f completionWriterFunc) Write(p []byte) (int, error) { return f(p) }

func TestCompletionOutputFailure(t *testing.T) {
	writeErr := errors.New("fixture output failure")
	for shell := range shellMarkers {
		for _, tc := range []struct {
			name     string
			accepted int
			writeErr error
			wantErr  error
		}{
			{name: "before writing", writeErr: writeErr, wantErr: writeErr},
			{name: "after a prefix", accepted: 32, writeErr: writeErr, wantErr: writeErr},
			{name: "short write", accepted: 32, wantErr: io.ErrShortWrite},
		} {
			t.Run(shell+"/"+tc.name, func(t *testing.T) {
				processOutput := captureStdout(t)
				var prefix, stderr bytes.Buffer
				var calls int
				output := completionWriterFunc(func(p []byte) (int, error) {
					calls++
					n, _ := prefix.Write(p[:min(tc.accepted, len(p))])
					return n, tc.writeErr
				})
				root := completionRoot(t, output, &stderr)
				root.SetArgs([]string{"completion", shell})
				if err := root.Execute(); !errors.Is(err, tc.wantErr) {
					t.Errorf("Execute() error = %v, want %v", err, tc.wantErr)
				}
				if calls != 1 || prefix.Len() != tc.accepted {
					t.Errorf("output got %d calls and %d bytes, want one call and %d bytes", calls, prefix.Len(), tc.accepted)
				}
				stat, err := processOutput.Stat()
				if err != nil {
					t.Fatal(err)
				}
				if stat.Size() != 0 || stderr.Len() != 0 {
					t.Errorf("failed output leaked %d bytes to process stdout and %d bytes to stderr", stat.Size(), stderr.Len())
				}
			})
		}
	}
}
