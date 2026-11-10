package workflows

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
)

// Regeneration must retire a previously generated workflow when its switch
// is turned off, including workflows gated by another switch or flavour.
func Test_Run_DisableOptionalWorkflows(t *testing.T) {
	for _, tc := range []struct {
		name string
		on   []string
		off  []string
		gone []string
	}{
		{"chart tests", nil, []string{"--chart-tests=false"}, []string{"chart_tests"}},
		{"readme links", nil, []string{"--check-readme-links=false"}, []string{"check_readme_links"}},
		{"reservations", []string{"--reservation-support"}, nil, []string{"reservation_command"}},
		{"helm docs", []string{"--helm-docs-regen"}, nil, []string{"helm-docs-regen"}},
		{"secret scanning", nil, []string{"--check-secrets=false"}, []string{"gitleaks"}},
		{"scorecard", nil, []string{"--run-security-scorecard=false"}, []string{"run_ossf_scorecard"}},
		{"actions analysis", []string{"--analyze-github-actions"}, nil, []string{"analyze-github-actions"}},
		{"techdocs", []string{"--publish-techdocs"}, nil, []string{"publish_techdocs"}},
		{"chart updates", []string{"--install-update-chart", "--upstream-sync-automation", "--dispatch-update-chart-events-repo=example"}, nil, []string{"update_chart", "sync_from_upstream", "dispatch_update_chart_events"}},
		{"upstream sync", []string{"--install-update-chart", "--upstream-sync-automation"}, []string{"--install-update-chart"}, []string{"sync_from_upstream"}},
		{"dispatch target", []string{"--install-update-chart", "--dispatch-update-chart-events-repo=example"}, []string{"--install-update-chart"}, []string{"dispatch_update_chart_events"}},
		{"app flavour", []string{"--helm-docs-regen", "--reservation-support"}, []string{"--flavour=generic"}, []string{"check_readme_links", "chart_tests", "check_values_schema", "helm-docs-regen", "reservation_command"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			run := func(options []string) {
				t.Helper()
				command, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
				require.NoError(t, err)
				args := []string{"--flavour=app"}
				// A changed flavour replaces the initial value, rather than
				// adding to pflag's repeatable flavour slice.
				if slices.Contains(options, "--flavour=generic") {
					args = nil
				}
				command.SetArgs(append(args, options...))
				require.NoError(t, command.Execute())
			}
			path := func(name string) string {
				return filepath.Join(".github", "workflows", "zz_generated."+name+".yaml")
			}
			run(tc.on)
			for _, name := range tc.gone {
				require.FileExists(t, path(name))
			}
			for _, name := range []string{"handwritten.yaml", "zz_generated.other-tool.yaml"} {
				require.NoError(t, os.WriteFile(filepath.Join(".github/workflows", name), []byte("name: keep\n"), 0o600))
			}
			require.NoError(t, os.WriteFile(".github/zizmor.base.yml", []byte("rules: {}\n"), 0o600))
			run(tc.off)
			for _, name := range tc.gone {
				require.NoFileExists(t, path(name))
			}
			require.FileExists(t, path("semantic_pull_request"))
			for _, name := range []string{"handwritten.yaml", "zz_generated.other-tool.yaml"} {
				got, err := os.ReadFile(filepath.Join(".github/workflows", name)) // #nosec G304 -- fixed fixture names inside t.TempDir
				require.NoError(t, err)
				require.Equal(t, "name: keep\n", string(got))
			}
			got, err := os.ReadFile(".github/zizmor.base.yml")
			require.NoError(t, err)
			require.Equal(t, "rules: {}\n", string(got))
			run(tc.on)
			for _, name := range tc.gone {
				require.FileExists(t, path(name))
			}
		})
	}
}

func Test_Run_ForkKeepsExistingWorkflows(t *testing.T) {
	for _, release := range []string{releaseWorkflowLegacy, releaseWorkflowAutoRelease} {
		t.Run(release, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(".github/workflows", 0o750))
			const path = ".github/workflows/zz_generated.reservation_command.yaml"
			require.NoError(t, os.WriteFile(path, []byte("name: upstream\n"), 0o600))
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			command, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			command.SetArgs([]string{"--flavour=fork", "--release-workflow=" + release, "--repo-name=fixture"})
			require.NoError(t, command.Execute())
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "name: upstream\n", string(got))
		})
	}
}
