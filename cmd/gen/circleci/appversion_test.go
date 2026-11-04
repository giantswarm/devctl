package circleci

import (
	"io"
	"os"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// The CLI default is true, but an omitted flag must leave appVersion's choice
// to the repository shape. Exercise real flag parsing and inspect every
// emitted chart job, rather than constructing the input package's bool pointer.
func Test_Run_ChartAppVersionFlags(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dockerfile bool
		flags      []string
		keep       bool
	}{
		{name: "chart-only default", keep: true},
		{name: "root Dockerfile default", dockerfile: true},
		{name: "nested Dockerfile default", flags: []string{"--image-dockerfile=containers/Dockerfile"}},
		{name: "explicit false overrides image derivation", dockerfile: true, flags: []string{"--override-chart-app-version=false"}, keep: true},
		{name: "explicit true overrides chart derivation", flags: []string{"--override-chart-app-version=true"}},
		{name: "bare modern flag is explicit true", flags: []string{"--override-chart-app-version"}},
		{name: "legacy keep flag", dockerfile: true, flags: []string{"--keep-chart-app-version"}, keep: true},
		{name: "legacy false keeps image derivation", dockerfile: true, flags: []string{"--keep-chart-app-version=false"}},
		{name: "legacy false keeps chart derivation", flags: []string{"--keep-chart-app-version=false"}, keep: true},
		{name: "explicit true wins over legacy keep", dockerfile: true, flags: []string{"--keep-chart-app-version", "--override-chart-app-version=true"}},
		{name: "explicit false wins over legacy false", dockerfile: true, flags: []string{"--override-chart-app-version=false", "--keep-chart-app-version=false"}, keep: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.dockerfile {
				require.NoError(t, os.WriteFile("Dockerfile", []byte("FROM scratch\n"), 0o600))
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			command, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			// Include branch publication so all three chart jobs must agree.
			args := []string{"--repo-name=fixture-app", "--flavour=app", "--branch-publish", "--skip-ats"}
			command.SetArgs(append(args, tc.flags...))
			require.NoError(t, command.Execute())
			data, err := os.ReadFile(".circleci/workflows.yml")
			require.NoError(t, err)
			var pipeline struct {
				Workflows map[string]struct {
					Jobs []map[string]struct {
						Name               string `json:"name"`
						OverrideAppVersion *bool  `json:"override_app_version"`
					} `json:"jobs"`
				} `json:"workflows"`
			}
			require.NoError(t, yaml.Unmarshal(data, &pipeline))
			var chartJobs []string
			for _, workflow := range pipeline.Workflows {
				for _, entry := range workflow.Jobs {
					job, ok := entry["architect/push-to-app-catalog"]
					if !ok {
						continue
					}
					chartJobs = append(chartJobs, job.Name)
					// The orb stamps appVersion unless explicitly told false.
					stamps := job.OverrideAppVersion == nil || *job.OverrideAppVersion
					require.Equal(t, !tc.keep, stamps, "chart job %s", job.Name)
				}
			}
			require.ElementsMatch(t, []string{"build-chart", "push-chart", "push-chart-release"}, chartJobs)
		})
	}
}
