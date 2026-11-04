package circleci

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

func Test_ChartTestRequiresCustomMerge(t *testing.T) {
	const custom = `version: 2.1
jobs:
  "true":
    docker:
    - image: cimg/base:current
    steps:
    - run: echo ready
workflows:
  build:
    jobs:
    - architect/push-to-registries:
        name: push-buzz-minio-to-registries
        image: giantswarm/buzz-minio
        persist-build-version: false
        split-china-push: true
        filters:
          branches:
            ignore:
            - main
            - /^release-v?[0-9]+(\.[0-9]+)?\.x$/
    - "true":
        filters:
          branches:
            ignore: main
`
	testCases := []struct {
		name          string
		hasDockerfile bool
		branchPublish bool
	}{
		{name: "case 0: chart with only custom images"},
		{name: "case 1: generated image validates without pushing", hasDockerfile: true},
		{name: "case 2: generated and custom images publish", hasDockerfile: true, branchPublish: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := render(t, Config{
				RepoName:          "buzz",
				Flavours:          gen.FlavourSlice{gen.FlavourApp},
				HasDockerfile:     tc.hasDockerfile,
				BranchPublish:     tc.branchPublish,
				ATSOnRelease:      true,
				ChartTestRequires: []string{"build-chart", "push-buzz-minio-to-registries", "true", "push-buzz-minio-to-registries", "build-chart"},
			})
			var pipeline struct {
				Workflows map[string]struct {
					Jobs []map[string]struct {
						Name     string         `json:"name"`
						Requires []string       `json:"requires"`
						Filters  map[string]any `json:"filters"`
					} `json:"jobs"`
				} `json:"workflows"`
			}
			data, err := yaml.YAMLToJSON([]byte(yqMerge(t, got, custom)))
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(data, &pipeline))
			seen := map[string]int{}
			for _, entry := range pipeline.Workflows["build"].Jobs {
				for key, job := range entry {
					name := job.Name
					if name == "" {
						name = key
					}
					seen[name]++
					switch name {
					case "execute-chart-tests":
						want := []string{"build-chart"}
						if tc.hasDockerfile && tc.branchPublish {
							want = append(want, "push-to-registries")
						}
						want = append(want, "push-buzz-minio-to-registries", "true")
						require.Equal(t, want, job.Requires)
						require.Equal(t, map[string]any{"branches": map[string]any{"ignore": []any{"main", releaseBranchesFilter}}}, job.Filters)
					case "execute-chart-tests-release":
						want := []string{"build-chart"}
						if tc.hasDockerfile {
							want = append(want, "push-to-registries-release")
						}
						require.Equal(t, want, job.Requires)
						require.Equal(t, map[string]any{"tags": map[string]any{"only": "/^v.*/"}, "branches": map[string]any{"ignore": "/.*/"}}, job.Filters)
					}
				}
			}
			for _, name := range []string{"execute-chart-tests", "execute-chart-tests-release", "push-buzz-minio-to-registries", "true"} {
				require.Equal(t, 1, seen[name], "workflow job %s", name)
			}
		})
	}
}

func Test_ChartTestRequiresRejects(t *testing.T) {
	testCases := []struct {
		name   string
		config Config
	}{
		{name: "case 0: no chart", config: Config{Language: gen.LanguageGo}},
		{name: "case 1: chart tests disabled", config: Config{Flavours: gen.FlavourSlice{gen.FlavourApp}, SkipATS: true}},
		{name: "case 2: template chart", config: Config{Flavours: gen.FlavourSlice{gen.FlavourApp}, ComponentType: ComponentTypeTemplate, Team: "bumblebee"}},
		{name: "case 3: multi-image repository", config: Config{Flavours: gen.FlavourSlice{gen.FlavourMultiImage}}},
		{name: "case 4: empty job", config: Config{Flavours: gen.FlavourSlice{gen.FlavourApp}, ChartTestRequires: []string{""}}},
		{name: "case 5: whitespace job", config: Config{Flavours: gen.FlavourSlice{gen.FlavourApp}, ChartTestRequires: []string{" "}}},
		{name: "case 6: padded job", config: Config{Flavours: gen.FlavourSlice{gen.FlavourApp}, ChartTestRequires: []string{" push-image"}}},
		{name: "case 7: self dependency", config: Config{Flavours: gen.FlavourSlice{gen.FlavourApp}, ChartTestRequires: []string{"execute-chart-tests"}}},
		{name: "case 8: multiline job", config: Config{Flavours: gen.FlavourSlice{gen.FlavourApp}, ChartTestRequires: []string{"push\nimage"}}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := tc.config
			config.RepoName = "buzz"
			if config.ChartTestRequires == nil {
				config.ChartTestRequires = []string{"push-buzz-minio-to-registries"}
			}
			_, err := New(config)
			require.True(t, IsInvalidConfig(err), "expected invalidConfigError, got %v", err)
			require.ErrorContains(t, err, "chart_tests.requires")
		})
	}
}
