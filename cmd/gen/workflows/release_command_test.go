package workflows_test

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/gen/workflows"
)

// A release-mode change must retire the previous flow, including when two
// different kinds of Auto Release share the same destination filename.
func Test_Command_ReleaseTransitions(t *testing.T) {
	args := map[string][]string{
		"legacy": {"--flavour=generic"}, // The public command defaults to legacy.
		"single": {"--flavour=generic", "--release-workflow=auto-release", "--repo-name=fixture"},
		"multi":  {"--flavour=multi-image", "--release-workflow=auto-release"},
	}
	for _, tc := range []struct {
		name  string
		modes []string
	}{
		{"case 0: single-line round trip", []string{"legacy", "single", "legacy"}},
		{"case 1: multi-image round trip", []string{"single", "multi", "single"}},
		{"case 2: legacy and multi-image round trip", []string{"legacy", "multi", "legacy"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(".github/workflows", 0o750))
			preserved := map[string]string{
				".github/workflows/custom.yaml":             "name: Custom\n",
				".github/workflows/zz_generated.other.yaml": "name: Another generator\n",
				"CHANGELOG.md": "# Changelog\n",
			}
			for path, content := range preserved {
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			}

			for _, mode := range tc.modes {
				// Both obsolete files may survive an older devctl. Every mode
				// must remove them without touching unrelated workflow files.
				for _, path := range []string{
					".github/workflows/auto-release.yaml",
					".github/workflows/zz_generated.changelog.yaml",
				} {
					require.NoError(t, os.WriteFile(path, []byte("name: Retired\n"), 0o600))
				}
				require.NoError(t, executeReleaseCommand(t, args[mode]...))
				got := releaseCheckoutSnapshot(t)
				wantPaths := []string{
					".github/workflows/custom.yaml",
					".github/workflows/zz_generated.other.yaml",
					".github/workflows/zz_generated.semantic_pull_request.yaml",
					"CHANGELOG.md",
				}
				switch mode {
				case "legacy":
					wantPaths = append(wantPaths,
						".github/workflows/zz_generated.create_release.yaml",
						".github/workflows/zz_generated.create_release_pr.yaml",
						".github/workflows/zz_generated.validate_changelog.yaml",
					)
					require.Contains(t, got[".github/workflows/zz_generated.create_release.yaml"], "name: Create Release\n")
					require.Contains(t, got[".github/workflows/zz_generated.create_release_pr.yaml"], "name: Create Release PR\n")
					require.Contains(t, got[".github/workflows/zz_generated.validate_changelog.yaml"], "name: Validate changelog\n")
				case "single":
					wantPaths = append(wantPaths, ".github/workflows/zz_generated.auto_release.yaml", "cliff.toml")
					require.Contains(t, got["cliff.toml"], `repo = "fixture"`)
					// Only the single-line flow supports candidate promotion.
					require.Contains(t, got[".github/workflows/zz_generated.auto_release.yaml"], "options: [auto, stable]")
					require.NotContains(t, got[".github/workflows/zz_generated.auto_release.yaml"], "IMAGES_DIR:")
				case "multi":
					wantPaths = append(wantPaths, ".github/workflows/zz_generated.auto_release.yaml")
					require.Contains(t, got[".github/workflows/zz_generated.auto_release.yaml"], "IMAGES_DIR: toolchains")
					require.NotContains(t, got[".github/workflows/zz_generated.auto_release.yaml"], "options: [auto, stable]")
				}
				var gotPaths []string
				for path := range got {
					if !strings.HasSuffix(path, "/") {
						gotPaths = append(gotPaths, path)
					}
				}
				require.ElementsMatch(t, wantPaths, gotPaths, "release mode %s", mode)
				for path, content := range preserved {
					require.Equal(t, content, got[path], "preserve %s", path)
				}

				// Repeating the same mode must leave the checkout unchanged.
				require.NoError(t, executeReleaseCommand(t, args[mode]...))
				require.Equal(t, got, releaseCheckoutSnapshot(t), "repeat release mode %s", mode)
			}
		})
	}
}

// Refusing a release configuration must precede all workflow writes and
// deletions, for a new directory as well as a repository with release files.
func Test_Command_RejectsReleaseOptionsBeforeWrites(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"case 0: missing flavour", nil, "--flavour must be one of"},
		{"case 1: unknown release flow", []string{"--flavour=generic", "--release-workflow=unknown"}, "--release-workflow must be one of"},
		{"case 2: maintenance on a non-fork", []string{"--flavour=generic", "--release-workflow=auto-release", "--maintenance-branches"}, "--maintenance-branches is only valid"},
		{"case 3: maintenance on legacy fork", []string{"--flavour=fork", "--maintenance-branches"}, "--maintenance-branches is only valid"},
		{"case 4: candidates on legacy", []string{"--flavour=generic", "--patch-candidates"}, "--patch-candidates is only valid"},
		{"case 5: multi-image on legacy", []string{"--flavour=multi-image"}, "requires --release-workflow=auto-release"},
		{"case 6: multi-image candidates", []string{"--flavour=multi-image", "--release-workflow=auto-release", "--patch-candidates"}, "--patch-candidates does not apply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, initial := range []string{"empty", "existing"} {
				t.Run(initial, func(t *testing.T) {
					t.Chdir(t.TempDir())
					if initial == "existing" {
						require.NoError(t, os.MkdirAll(".github/workflows", 0o750))
						for _, path := range []string{
							"cliff.toml",
							".github/workflows/auto-release.yaml",
							".github/workflows/zz_generated.auto_release.yaml",
							".github/workflows/zz_generated.changelog.yaml",
							".github/workflows/zz_generated.create_release.yaml",
							".github/workflows/zz_generated.create_release_pr.yaml",
							".github/workflows/zz_generated.validate_changelog.yaml",
							".github/workflows/zz_generated.semantic_pull_request.yaml",
						} {
							require.NoError(t, os.WriteFile(path, []byte("keep "+path+"\n"), 0o600))
						}
					}
					before := releaseCheckoutSnapshot(t)
					err := executeReleaseCommand(t, tc.args...)
					require.ErrorContains(t, err, tc.want)
					require.True(t, workflows.IsInvalidFlag(err), "preserve the command's invalid-flag error kind")
					require.Equal(t, before, releaseCheckoutSnapshot(t))
				})
			}
		})
	}
}

func executeReleaseCommand(t *testing.T, args ...string) error {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	command, err := workflows.New(workflows.Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	require.NoError(t, err)
	command.SilenceUsage = true
	command.SilenceErrors = true
	// These optional workflows have separate transition coverage. A fresh
	// command per invocation also reproduces the CLI's flag defaults.
	command.SetArgs(append([]string{"--check-secrets=false", "--run-security-scorecard=false"}, args...))
	return command.Execute()
}

// Directory entries are included so a rejected command cannot silently
// create directories even if it leaves the files alone.
func releaseCheckoutSnapshot(t *testing.T) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		key := filepath.ToSlash(path)
		if d.IsDir() {
			entries[key+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G304 -- walking only the test's temporary checkout
		if err != nil {
			return err
		}
		entries[key] = string(data)
		return nil
	})
	require.NoError(t, err)
	return entries
}
