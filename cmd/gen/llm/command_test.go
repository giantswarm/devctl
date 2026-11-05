package llm_test

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/gen/llm"
)

// Forks carry upstream's files. The reposetup caller skips this command for
// forks, so its rendering tests cannot protect a direct invocation's opt-out.
func TestCommandForkLeavesCheckoutUntouched(t *testing.T) {
	for _, language := range []string{"go", "generic"} {
		t.Run(language, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				files map[string]string
			}{
				{name: "empty checkout"},
				{
					name: "existing upstream rules",
					files: map[string]string{
						".cursor/rules/zz_generated.base-llm-rules.mdc": "upstream base instructions\n",
						".cursor/rules/zz_generated.go-llm-rules.mdc":   "upstream Go instructions\n",
						".cursor/rules/custom.mdc":                      "custom instructions\n",
						"README.md":                                     "upstream documentation\n",
					},
				},
				{
					name:  "output directory blocked",
					files: map[string]string{".cursor": "upstream regular file\n"},
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Chdir(t.TempDir())
					for name, content := range tc.files {
						require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o750))
						require.NoError(t, os.WriteFile(name, []byte(content), 0o600))
					}
					before := checkoutContents(t)

					require.NoError(t, executeCommand(t, "--flavour", "fork", "--language", language))

					require.Equal(t, before, checkoutContents(t), "a fork invocation must leave the checkout untouched")
				})
			}
		})
	}
}

// Positive controls keep the fork checks from accepting a command that skips
// every checkout. Go adds its supplement; the default needs no flags at all.
func TestCommandNonForkWritesRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		wantGo bool
	}{
		{name: "default base rules"},
		{name: "Go rules", args: []string{"-f", "app", "-l", "go"}, wantGo: true},
		{name: "generic rules", args: []string{"--flavour", "generic", "--language", "generic"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, executeCommand(t, tc.args...))

			base, err := os.ReadFile(".cursor/rules/zz_generated.base-llm-rules.mdc")
			require.NoError(t, err)
			require.Contains(t, string(base), "alwaysApply: true")
			require.Contains(t, string(base), "Instructions for AI/LLM assistants")
			if tc.wantGo {
				require.Contains(t, string(base), "### Go Development")
				goRules, err := os.ReadFile(".cursor/rules/zz_generated.go-llm-rules.mdc")
				require.NoError(t, err)
				require.Contains(t, string(goRules), "Go language-specific development guidelines and patterns")
			} else {
				require.NotContains(t, string(base), "### Go Development")
				require.NoFileExists(t, ".cursor/rules/zz_generated.go-llm-rules.mdc")
			}
		})
	}
}

func TestCommandNonForkReportsBlockedOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile(".cursor", []byte("existing regular file\n"), 0o600))
	before := checkoutContents(t)

	require.Error(t, executeCommand(t, "--flavour", "generic", "--language", "go"))

	require.Equal(t, before, checkoutContents(t))
}

func TestCommandDryRunReportsFindingsWithoutChanges(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		args  []string
		want  []string
	}{
		{
			name: "new repository",
			want: []string{"gen llm would proceed: no findings"},
		},
		{
			name:  "unmigrated repository",
			files: map[string]string{"CLAUDE.md": "Run make test.\n"},
			want: []string{
				"gen llm would skip this repository:",
				"CLAUDE.md: holds instructions that are not in a devctl-managed block yet\n  fix: run devctl gen llm --migrate-instructions and review the result",
			},
		},
		{
			name:  "migration of the same repository",
			files: map[string]string{"CLAUDE.md": "Run make test.\n"},
			args:  []string{"--migrate-instructions"},
			want:  []string{"gen llm would proceed: no findings"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(name, []byte(content), 0o600))
			}
			before := checkoutContents(t)

			var stdout bytes.Buffer
			err := executeCommandTo(t, &stdout, append([]string{"--flavour", "app", "--language", "go", "--dry-run"}, tc.args...)...)
			require.NoError(t, err)

			for _, want := range tc.want {
				require.Contains(t, stdout.String(), want)
			}
			require.Equal(t, before, checkoutContents(t))
		})
	}
}

// The migration itself is not implemented yet.
func TestCommandMigrationRequiresDryRun(t *testing.T) {
	t.Chdir(t.TempDir())
	before := checkoutContents(t)

	require.ErrorContains(t, executeCommand(t, "--migrate-instructions"), "--dry-run")

	require.Equal(t, before, checkoutContents(t))
}

func executeCommand(t *testing.T, args ...string) error {
	t.Helper()
	return executeCommandTo(t, io.Discard, args...)
}

func executeCommandTo(t *testing.T, stdout io.Writer, args ...string) error {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	cmd, err := llm.New(llm.Config{Logger: logger, Stdout: stdout, Stderr: io.Discard})
	require.NoError(t, err)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	// An explicit empty slice keeps Cobra from reading the test process's flags.
	cmd.SetArgs(append([]string{}, args...))
	return cmd.ExecuteContext(t.Context())
}

// Include directory entries so an otherwise empty .cursor/rules is a mutation.
func checkoutContents(t *testing.T) map[string]string {
	t.Helper()
	root, err := os.OpenRoot(".")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	contents := make(map[string]string)
	err = filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			contents[path] = "directory"
			return nil
		}
		data, err := root.ReadFile(path)
		if err != nil {
			return err
		}
		contents[path] = string(data)
		return nil
	})
	require.NoError(t, err)
	return contents
}
