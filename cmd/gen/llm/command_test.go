package llm_test

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
// every checkout. Language and flavour shape the block; the default needs no
// flags at all.
func TestCommandWritesAgentsMd(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		wantGo  bool
		wantApp bool
	}{
		{name: "default"},
		{name: "Go app", args: []string{"-f", "app", "-l", "go"}, wantGo: true, wantApp: true},
		{name: "generic", args: []string{"--flavour", "generic", "--language", "generic"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(".cursor/rules", 0o750))
			require.NoError(t, os.WriteFile(".cursor/rules/zz_generated.base-llm-rules.mdc", []byte("old\n"), 0o600))
			require.NoError(t, os.WriteFile(".cursor/rules/zz_generated.go-llm-rules.mdc", []byte("old\n"), 0o600))

			require.NoError(t, executeCommand(t, tc.args...))

			agents, err := os.ReadFile("AGENTS.md")
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(string(agents), "<!-- BEGIN devctl-managed: edits in this block are overwritten -->\n<!-- markdownlint-disable-file MD041 -->\n\n## Giant Swarm conventions\n"))
			require.True(t, strings.HasSuffix(string(agents), ".\n\n<!-- END devctl-managed -->\n\n## This repository\n"))
			require.Equal(t, tc.wantGo, strings.Contains(string(agents), "goimports -local"))
			require.Equal(t, tc.wantApp, strings.Contains(string(agents), "`gs-appdev`"))
			require.NoDirExists(t, ".cursor")
		})
	}
}

// Alignment runs every generator and fails the repository on any error, so a
// repository gen llm cannot handle is a warning, not a failure.
func TestCommandSkipsUnmigratedRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("CLAUDE.md", []byte("Run make test.\n"), 0o600))
	require.NoError(t, os.MkdirAll(".cursor/rules", 0o750))
	require.NoError(t, os.WriteFile(".cursor/rules/zz_generated.base-llm-rules.mdc", []byte("old\n"), 0o600))
	before := checkoutContents(t)

	var stderr bytes.Buffer
	require.NoError(t, executeCommandWith(t, io.Discard, &stderr))

	require.Contains(t, stderr.String(), "gen llm skipped this repository:\nCLAUDE.md: holds instructions that are not in a devctl-managed block yet\n  fix: run devctl gen llm --migrate-instructions and review the result\n")
	require.Equal(t, before, checkoutContents(t))
}

// A migration that was asked for and did not happen is an error, so that a
// script running it notices.
func TestCommandMigrationFailsWhenSkipped(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("CONTRIBUTING.md", []byte("Contributing.\n"), 0o600))
	require.NoError(t, os.Symlink("CONTRIBUTING.md", "AGENTS.md"))
	before := checkoutContents(t)

	var stderr bytes.Buffer
	err := executeCommandWith(t, io.Discard, &stderr, "--migrate-instructions")

	require.ErrorContains(t, err, "not migrated")
	require.Contains(t, stderr.String(), "gen llm skipped this repository:\nAGENTS.md: is a link to CONTRIBUTING.md")
	require.Equal(t, before, checkoutContents(t))
}

func TestCommandDryRunOfUpToDateRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, executeCommand(t, "-l", "go"))

	var stdout bytes.Buffer
	require.NoError(t, executeCommandTo(t, &stdout, "-l", "go", "--dry-run"))

	require.Equal(t, "gen llm would make no changes\n", stdout.String())
}

func TestCommandMigratesInstructions(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("CLAUDE.md", []byte("Run make test.\n"), 0o600))

	require.NoError(t, executeCommand(t, "--migrate-instructions"))

	agents, err := os.ReadFile("AGENTS.md")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(string(agents), "<!-- END devctl-managed -->\n\nRun make test.\n"))
	claude, err := os.ReadFile("CLAUDE.md")
	require.NoError(t, err)
	require.Equal(t, "@AGENTS.md\n", string(claude))
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
			want: []string{"gen llm would make these changes:\n- create AGENTS.md with the devctl-managed block\n"},
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
			name:  "fork",
			files: map[string]string{"CLAUDE.md": "Run make test.\n"},
			args:  []string{"--flavour", "fork"},
			want:  []string{"gen llm would do nothing: nothing is generated for a fork"},
		},
		{
			name:  "migration of the same repository",
			files: map[string]string{"CLAUDE.md": "Run make test.\n"},
			args:  []string{"--migrate-instructions"},
			want: []string{
				"gen llm would make these changes:\n" +
					"- move the content of CLAUDE.md to AGENTS.md, below the devctl-managed block\n" +
					"- replace CLAUDE.md with an import of AGENTS.md\n",
			},
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

func executeCommand(t *testing.T, args ...string) error {
	t.Helper()
	return executeCommandTo(t, io.Discard, args...)
}

func executeCommandTo(t *testing.T, stdout io.Writer, args ...string) error {
	t.Helper()
	return executeCommandWith(t, stdout, io.Discard, args...)
}

func executeCommandWith(t *testing.T, stdout, stderr io.Writer, args ...string) error {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	cmd, err := llm.New(llm.Config{Logger: logger, Stdout: stdout, Stderr: stderr})
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
