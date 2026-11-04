package gen

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
)

// In-process callers such as reposetup execute the generators with a context.
// A call that is already canceled must not change the checkout or start work.
// Apptest additionally covers its Go subprocesses in its own package tests.
func Test_CanceledGeneratorsLeaveCheckoutUntouched(t *testing.T) {
	for _, args := range [][]string{
		{"ami", "--dir=fixture"}, // Missing synthetic AWS config prevents a live request even on a regression.
		{"circleci", "--repo-name=fixture", "--flavour=app", "--skip-ats"},
		{"dependabot", "--ecosystems=docker"},
		{"llm"},
		{"makefile", "--flavour=generic", "--language=generic"},
		{"plans", "--flavour=plans"},
		{"precommit", "--language=generic"},
		{"renovate", "--language=generic"},
		{"renovate", "reviewers", "--reviewers=team:fixture"},
		{"workflows", "--flavour=generic"},
	} {
		t.Run(args[0]+"/"+args[len(args)-1], func(t *testing.T) {
			t.Chdir(t.TempDir())
			const original = "{reviewers: ['team:original']}\n"
			require.NoError(t, os.WriteFile("renovate.json5", []byte(original), 0o600))
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			command, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			command.SilenceErrors = true
			command.SilenceUsage = true
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			command.SetArgs(args)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err = command.ExecuteContext(ctx)
			require.ErrorIs(t, err, context.Canceled)
			var files []string
			require.NoError(t, filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					files = append(files, path)
				}
				return err
			}))
			require.Equal(t, []string{"renovate.json5"}, files)
			got, err := os.ReadFile("renovate.json5")
			require.NoError(t, err)
			require.Equal(t, original, string(got))
		})
	}
}
