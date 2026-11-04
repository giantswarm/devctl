package circleci

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func Test_Run_ChartTestRequires(t *testing.T) {
	testCases := []struct {
		name   string
		custom string
		want   []string
	}{
		{
			name: "case 0: absent config keeps existing dependencies",
			want: []string{"build-chart", "push-to-registries"},
		},
		{
			name:   "case 1: empty config keeps existing dependencies",
			custom: "chart_tests:\n  requires: []\n",
			want:   []string{"build-chart", "push-to-registries"},
		},
		{
			name:   "case 2: custom jobs extend branch chart-test dependencies",
			custom: "chart_tests:\n  requires:\n  - push-buzz-minio-to-registries\n  - 'true'\n  - build-chart\n  - push-to-registries\n  - push-buzz-minio-to-registries\n",
			want:   []string{"build-chart", "push-to-registries", "push-buzz-minio-to-registries", "true"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(".circleci", 0o750))
			require.NoError(t, os.WriteFile("Dockerfile", []byte("FROM scratch\n"), 0o600))
			const configPath = ".circleci/config.custom.yml"
			if tc.custom != "" {
				require.NoError(t, os.WriteFile(configPath, []byte(tc.custom), 0o600))
			}
			const jobs = "jobs:\n  'true':\n    docker:\n    - image: cimg/base:current\n    steps:\n    - run: echo ready\nworkflows:\n  build:\n    jobs:\n    - architect/push-to-registries:\n        name: push-buzz-minio-to-registries\n        image: giantswarm/buzz-minio\n        persist-build-version: false\n    - 'true'\n"
			require.NoError(t, os.WriteFile(".circleci/custom.yml", []byte(jobs), 0o600))

			cmd := &cobra.Command{}
			f := &flag{}
			f.Init(cmd)
			require.NoError(t, cmd.ParseFlags([]string{"--repo-name=buzz", "--language=generic", "--flavour=app", "--branch-publish", "--ats-on-release"}))
			require.NoError(t, (&runner{flag: f}).Run(cmd, nil))

			data, err := os.ReadFile(".circleci/workflows.yml")
			require.NoError(t, err)
			var pipeline struct {
				Workflows map[string]struct {
					Jobs []map[string]struct {
						Name     string   `json:"name"`
						Requires []string `json:"requires"`
					} `json:"jobs"`
				} `json:"workflows"`
			}
			data, err = yaml.YAMLToJSON(data)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(data, &pipeline))
			seen := map[string]int{}
			for _, entry := range pipeline.Workflows["build"].Jobs {
				for _, job := range entry {
					seen[job.Name]++
					switch job.Name {
					case "execute-chart-tests":
						require.Equal(t, tc.want, job.Requires)
					case "execute-chart-tests-release":
						require.Equal(t, []string{"build-chart", "push-to-registries-release"}, job.Requires)
					}
				}
			}
			require.Equal(t, 1, seen["execute-chart-tests"])
			require.Equal(t, 1, seen["execute-chart-tests-release"])
			data, err = os.ReadFile(".circleci/custom.yml")
			require.NoError(t, err)
			require.Equal(t, jobs, string(data))
			if tc.custom != "" {
				data, err = os.ReadFile(configPath)
				require.NoError(t, err)
				require.Equal(t, tc.custom, string(data))
			}
		})
	}
}

func Test_Run_ChartTestRequiresErrorsLeaveFilesUntouched(t *testing.T) {
	testCases := []struct {
		name      string
		custom    string
		directory bool
		skipATS   bool
		wantErr   string
	}{
		{name: "case 0: malformed YAML", custom: "chart_tests: [", wantErr: "config.custom.yml"},
		{name: "case 1: unknown key", custom: "chart_test: {}", wantErr: "config.custom.yml"},
		{name: "case 2: requires is not a list", custom: "chart_tests:\n  requires: push-image", wantErr: "config.custom.yml"},
		{name: "case 3: duplicate key", custom: "chart_tests: {}\nchart_tests: {}", wantErr: "config.custom.yml"},
		{name: "case 4: config cannot be read", directory: true, wantErr: "config.custom.yml"},
		{name: "case 5: empty job name", custom: "chart_tests:\n  requires: ['']", wantErr: "chart_tests.requires"},
		{name: "case 6: tests are disabled", custom: "chart_tests:\n  requires: [push-image]", skipATS: true, wantErr: "chart_tests.requires"},
		{name: "case 7: self dependency", custom: "chart_tests:\n  requires: [execute-chart-tests]", wantErr: "chart_tests.requires"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(".circleci", 0o750))
			const configPath = ".circleci/config.custom.yml"
			const original = "version: 2.1\n# original\n"
			if tc.directory {
				require.NoError(t, os.Mkdir(configPath, 0o750))
			} else {
				require.NoError(t, os.WriteFile(configPath, []byte(tc.custom), 0o600))
			}
			for _, path := range []string{".circleci/config.yml", ".circleci/workflows.yml", ".circleci/custom.yml"} {
				require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
			}
			cmd := &cobra.Command{}
			f := &flag{}
			f.Init(cmd)
			args := []string{"--repo-name=buzz", "--language=generic", "--flavour=app"}
			if tc.skipATS {
				args = append(args, "--skip-ats")
			}
			require.NoError(t, cmd.ParseFlags(args))
			err := (&runner{flag: f}).Run(cmd, nil)
			require.ErrorContains(t, err, tc.wantErr)
			for _, path := range []string{".circleci/config.yml", ".circleci/workflows.yml", ".circleci/custom.yml"} {
				data, err := os.ReadFile(path) // #nosec G304 -- fixed fixture paths above, inside t.TempDir
				require.NoError(t, err)
				require.Equal(t, original, string(data), "existing %s", path)
			}
			require.NoDirExists(t, "tests/ats")
		})
	}
}
