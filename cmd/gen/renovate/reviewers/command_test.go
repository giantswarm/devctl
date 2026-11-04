package reviewers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"

	renovatecmd "github.com/giantswarm/devctl/v8/cmd/gen/renovate"
	"github.com/giantswarm/devctl/v8/cmd/gen/renovate/reviewers"
	"github.com/giantswarm/devctl/v8/pkg/renovate"
)

// Exercise the registered child command: its flags and validation must stay
// independent of the parent's generation flags and preserve repo-owned files.
func TestCommandEditsExistingConfig(t *testing.T) {
	cases := []struct {
		name      string
		files     map[string]string
		args      []string
		selected  string
		want      string
		reviewers []string
	}{
		{
			name: "prefers json5 and replaces only the top-level value",
			files: map[string]string{
				"renovate.json5": "// hand maintained\n{\n  reviewers: /* owners */ ['old'],\n  enabled: false,\n  packageRules: [{reviewers: ['nested']}],\n}\n",
				"renovate.json":  "{\"reviewers\":[\"leave-this-alone\"]}\n",
			},
			args:      []string{"--reviewers", "team:fixture-a,fixture-user", "-r", "team:fixture-b"},
			selected:  "renovate.json5",
			want:      "// hand maintained\n{\n  reviewers: /* owners */ ['team:fixture-a', 'fixture-user', 'team:fixture-b'],\n  enabled: false,\n  packageRules: [{reviewers: ['nested']}],\n}\n",
			reviewers: []string{"team:fixture-a", "fixture-user", "team:fixture-b"},
		},
		{
			name: "inserts into an existing json5 config",
			files: map[string]string{
				"renovate.json5": "// custom preset\n{\n\textends: ['local>fixture'],\n}\n",
			},
			args:      []string{"-r", "fixture-user", "--reviewers", "team:fixture"},
			selected:  "renovate.json5",
			want:      "// custom preset\n{\n\treviewers: ['fixture-user', 'team:fixture'],\n\textends: ['local>fixture'],\n}\n",
			reviewers: []string{"fixture-user", "team:fixture"},
		},
		{
			name:      "falls back to json and keeps it strict JSON",
			files:     map[string]string{"renovate.json": "{}\n"},
			args:      []string{"-r", "fixture-user"},
			selected:  "renovate.json",
			want:      "{\n  \"reviewers\": [\"fixture-user\"]\n}\n",
			reviewers: []string{"fixture-user"},
		},
		{
			name: "keeps an already matching multiline value verbatim",
			files: map[string]string{
				"renovate.json5": "{\n  reviewers: [\n    'fixture-user',\n    \"team:fixture\",\n  ],\n}\n",
			},
			args:      []string{"-r", "fixture-user", "-r", "team:fixture"},
			selected:  "renovate.json5",
			want:      "{\n  reviewers: [\n    'fixture-user',\n    \"team:fixture\",\n  ],\n}\n",
			reviewers: []string{"fixture-user", "team:fixture"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFixtures(t, tc.files)
			wantFiles := snapshotFiles(t)
			wantFiles[tc.selected] = tc.want

			// A fresh command tree on each call matches separate CLI invocations.
			for range 2 {
				stdout, err := executeCommand(t, tc.args)
				require.NoError(t, err)
				require.Equal(t, "Set reviewers in "+tc.selected+" to ["+strings.Join(tc.reviewers, " ")+"]\n", stdout)
				require.Equal(t, wantFiles, snapshotFiles(t), "only the selected root configuration may change; no temporary files may remain")
			}
			if tc.selected == "renovate.json" {
				var parsed struct {
					Reviewers []string `json:"reviewers"`
				}
				require.NoError(t, json.Unmarshal([]byte(snapshotFiles(t)[tc.selected]), &parsed))
				require.Equal(t, tc.reviewers, parsed.Reviewers)
			}
		})
	}
}

func TestCommandRejectsWithoutChangingFiles(t *testing.T) {
	cases := []struct {
		name        string
		files       map[string]string
		args        []string
		isError     func(error) bool
		containsErr string
	}{
		{
			name:    "missing reviewers",
			files:   map[string]string{"renovate.json5": "{reviewers: ['old']}\n"},
			isError: reviewers.IsInvalidFlag,
		},
		{
			name:    "explicitly empty reviewers",
			files:   map[string]string{"renovate.json5": "{reviewers: ['old']}\n"},
			args:    []string{"--reviewers="},
			isError: reviewers.IsInvalidFlag,
		},
		{
			name:        "stray positional argument",
			files:       map[string]string{"renovate.json5": "{reviewers: ['old']}\n"},
			args:        []string{"-r", "fixture-user", "unexpected"},
			containsErr: "unexpected",
		},
		{
			name:        "malformed reviewer CSV",
			files:       map[string]string{"renovate.json5": "{reviewers: ['old']}\n"},
			args:        []string{"--reviewers=\"unterminated"},
			containsErr: "reviewers",
		},
		{
			name:    "no root configuration",
			args:    []string{"-r", "fixture-user"},
			isError: renovate.IsConfigNotFound,
		},
		{
			name: "invalid preferred config does not fall back to json",
			files: map[string]string{
				"renovate.json5": "[1, 2, 3]\n",
				"renovate.json":  "{\"reviewers\":[\"old\"]}\n",
			},
			args:    []string{"-r", "fixture-user"},
			isError: renovate.IsInvalidConfig,
		},
		{
			name:    "truncated root object",
			files:   map[string]string{"renovate.json5": "{ extends: ['fixture']\n"},
			args:    []string{"-r", "fixture-user"},
			isError: renovate.IsInvalidConfig,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFixtures(t, tc.files)
			before := snapshotFiles(t)

			stdout, err := executeCommand(t, tc.args)
			require.Error(t, err)
			if tc.isError != nil {
				require.True(t, tc.isError(err), "unexpected error: %v", err)
			}
			if tc.containsErr != "" {
				require.ErrorContains(t, err, tc.containsErr)
			}
			require.Empty(t, stdout, "a refused edit must not report success")
			require.Equal(t, before, snapshotFiles(t), "a refused edit must leave the checkout unchanged")
		})
	}
}

func executeCommand(t *testing.T, args []string) (string, error) {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	var stdout bytes.Buffer
	cmd, err := renovatecmd.New(renovatecmd.Config{Logger: logger, Stdout: &stdout, Stderr: io.Discard})
	require.NoError(t, err)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(append([]string{"reviewers"}, args...))
	err = cmd.Execute()
	return stdout.String(), err
}

func writeFixtures(t *testing.T, files map[string]string) {
	t.Helper()
	// Generation removes Dependabot's config; surgical editing must preserve it.
	require.NoError(t, os.Mkdir(".github", 0o750))
	require.NoError(t, os.WriteFile(".github/dependabot.yml", []byte("version: 2\n"), 0o600))
	for path, content := range files {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

// Include directories and unexpected temporary files in the snapshot, too.
func snapshotFiles(t *testing.T) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		if entry.IsDir() {
			files[filepath.ToSlash(path)+"/"] = ""
			return nil
		}
		content, err := os.ReadFile(path) // #nosec G304 G122 -- private t.TempDir contains only synthetic fixtures; no concurrent writers or symlinks
		if err != nil {
			return err
		}
		files[filepath.ToSlash(path)] = string(content)
		return nil
	})
	require.NoError(t, err)
	return files
}
