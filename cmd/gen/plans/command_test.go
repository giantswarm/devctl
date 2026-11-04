package plans_test

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"

	gencmd "github.com/giantswarm/devctl/v8/cmd/gen"
)

func Test_Command_FlavourGate(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{name: "case 0: no flavour"},
		{name: "case 1: another flavour", args: []string{"--flavour=generic"}},
		{name: "case 2: several other flavours", args: []string{"-f", "app,cli"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, existing := range []bool{false, true} {
				name := "empty checkout"
				if existing {
					name = "malformed settings"
				}
				t.Run(name, func(t *testing.T) {
					t.Chdir(t.TempDir())
					if existing {
						writeFixture(t, ".claude/settings.json", "{ malformed team settings\n")
					}
					before := checkout(t)

					require.NoError(t, executePlans(t, tc.args...))
					require.Equal(t, before, checkout(t), "skipping generation must not even create .claude")
				})
			}
		})
	}
}

func Test_Command_GeneratesSettings(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{name: "case 0: long flag", args: []string{"--flavour", "plans"}},
		{name: "case 1: short flag", args: []string{"-f", "plans"}},
		{name: "case 2: comma separated flavours", args: []string{"--flavour=generic,plans"}},
		{name: "case 3: repeated flags", args: []string{"--flavour=generic", "--flavour=plans"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, executePlans(t, tc.args...))

			got := checkout(t)
			settings := got[".claude/settings.json"]
			require.JSONEq(t, `{
  "enabledPlugins": {"gs-plans@giantswarm": true},
  "extraKnownMarketplaces": {
    "giantswarm": {"autoUpdate": true, "source": {"source": "github", "repo": "giantswarm/claude-code"}}
  }
}`, settings)
			require.Equal(t, map[string]string{".claude/": "", ".claude/settings.json": settings}, got)
		})
	}
}

func Test_Command_RegeneratesTeamSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFixture(t, "README.md", "team plans\n")
	writeFixture(t, ".claude/settings.local.json", "{\"model\": \"fixture-local\"}\n")
	writeFixture(t, ".claude/settings.json", `{
  "model": "fixture-team",
  "permissions": {"allow": ["Bash(make lint && make test)"], "deny": ["Read(.env)"]},
  "enabledPlugins": {"fixture@other": false, "gs-plans@giantswarm": false},
  "extraKnownMarketplaces": {
    "other": {"source": {"source": "github", "repo": "example/fixture"}},
    "giantswarm": {"autoUpdate": false, "source": {"source": "github", "repo": "example/outdated"}}
  }
}`)
	before := checkout(t)

	require.NoError(t, executePlans(t, "--flavour=plans"))
	after := checkout(t)
	settings := after[".claude/settings.json"]
	require.JSONEq(t, `{
  "model": "fixture-team",
  "permissions": {"allow": ["Bash(make lint && make test)"], "deny": ["Read(.env)"]},
  "enabledPlugins": {"fixture@other": false, "gs-plans@giantswarm": true},
  "extraKnownMarketplaces": {
    "other": {"source": {"source": "github", "repo": "example/fixture"}},
    "giantswarm": {"autoUpdate": true, "source": {"source": "github", "repo": "giantswarm/claude-code"}}
  }
}`, settings)
	require.Contains(t, settings, "Bash(make lint && make test)")
	before[".claude/settings.json"] = settings
	require.Equal(t, before, after, "only the shared settings file may change")

	// align-files and reposetup construct a fresh command for each invocation.
	require.NoError(t, executePlans(t, "--flavour=plans"))
	require.Equal(t, after, checkout(t), "a second generation must be byte-stable")
}

func Test_Command_RejectsInvalidArguments(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{name: "case 0: invalid flavour", args: []string{"--flavour=plan"}},
		{name: "case 1: invalid flavour after plans", args: []string{"--flavour=plans", "--flavour=plan"}},
		{name: "case 2: positional argument", args: []string{"--flavour=plans", "unexpected"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFixture(t, ".claude/settings.json", "{}\n")
			before := checkout(t)

			require.Error(t, executePlans(t, tc.args...))
			require.Equal(t, before, checkout(t))
		})
	}
}

func Test_Command_FileFailuresPreserveCheckout(t *testing.T) {
	testCases := []struct {
		name     string
		path     string
		contents string
		dir      string
	}{
		{name: "case 0: malformed JSON", path: ".claude/settings.json", contents: "{\"model\": \"fixture\",}\n"},
		{name: "case 1: trailing JSON", path: ".claude/settings.json", contents: "{} {}\n"},
		{name: "case 2: array instead of settings object", path: ".claude/settings.json", contents: "[]\n"},
		{name: "case 3: destination is a directory", dir: ".claude/settings.json"},
		{name: "case 4: parent is a file", path: ".claude", contents: "team file\n"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFixture(t, "README.md", "team plans\n")
			if tc.path != "" {
				writeFixture(t, tc.path, tc.contents)
			}
			if tc.dir != "" {
				require.NoError(t, os.MkdirAll(tc.dir, 0o750))
			}
			before := checkout(t)

			require.Error(t, executePlans(t, "--flavour=plans"))
			require.Equal(t, before, checkout(t), "a failed command must not truncate settings or change other files")
		})
	}
}

func executePlans(t *testing.T, args ...string) error {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	root, err := gencmd.New(gencmd.Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	require.NoError(t, err)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs(append([]string{"plans"}, args...))
	return root.ExecuteContext(t.Context())
}

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600)) // #nosec G304 -- synthetic paths in t.TempDir only
}

// checkout records directories as well as files to detect unintended creation
// of .claude on a skipped command, and loss of unrelated files on failure.
func checkout(t *testing.T) map[string]string {
	t.Helper()
	got := make(map[string]string)
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			got[filepath.ToSlash(path)+"/"] = ""
			return nil
		}
		content, err := os.ReadFile(path) // #nosec G304 -- synthetic paths in t.TempDir only
		if err != nil {
			return err
		}
		got[filepath.ToSlash(path)] = string(content)
		return nil
	})
	require.NoError(t, err)
	return got
}
