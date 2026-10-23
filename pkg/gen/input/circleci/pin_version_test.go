package circleci

import (
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	jobPinVersion = "architect/pin-version"
	jobBuildImage = "architect/build-image"
	pinVersion    = "pin-version"
)

// workflowJob is one entry of the build workflow's job list: the orb job (or
// repo-local job) it invokes and its parameters.
type workflowJob struct {
	job    string
	params map[string]any
}

func buildWorkflowJobs(t *testing.T, rendered string) []workflowJob {
	t.Helper()

	var doc struct {
		Workflows map[string]struct {
			Jobs []map[string]map[string]any `yaml:"jobs"`
		} `yaml:"workflows"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
		t.Fatalf("parse rendered workflows: %v\n%s", err, rendered)
	}

	var jobs []workflowJob
	for _, entry := range doc.Workflows["build"].Jobs {
		for job, params := range entry {
			jobs = append(jobs, workflowJob{job: job, params: params})
		}
	}
	return jobs
}

func (j workflowJob) requires() []string {
	var out []string
	if list, ok := j.params["requires"].([]any); ok {
		for _, r := range list {
			out = append(out, r.(string))
		}
	}
	return out
}

// Test_PinVersionGatesEveryImageAndChartJob verifies that a workflow building
// an image or a chart starts with one architect/pin-version job, which every
// image and chart job requires, and that every chart job attaches the
// workspace the pinned version lives in -- so every job of the workflow
// builds with one version, even when a promote tags a commit mid-run.
func Test_PinVersionGatesEveryImageAndChartJob(t *testing.T) {
	singleJob := nativeNodeConfig()
	singleJob.ImageNativeBuilds = false

	atsOnRelease := nativeNodeConfig()
	atsOnRelease.ImageNativeBuilds = false
	atsOnRelease.BranchPublish = false
	atsOnRelease.ATSOnRelease = true

	cases := map[string]Config{
		"backstage, single image job":    singleJob,
		"backstage, native image builds": nativeNodeConfig(),
		"backstage, ats on release":      atsOnRelease,
		"go image and chart": {
			RepoName:      repoMCPKubernetes,
			Language:      gen.LanguageGo,
			Flavours:      gen.FlavourSlice{gen.FlavourApp},
			HasDockerfile: true,
		},
		"chart only": {
			RepoName: repoSitesearch,
			Flavours: gen.FlavourSlice{gen.FlavourApp},
		},
		"image only": {
			RepoName:      repoMCPKubernetes,
			Language:      gen.LanguageGo,
			HasDockerfile: true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := render(t, c)
			jobs := buildWorkflowJobs(t, got)

			var pins, gated int
			for _, j := range jobs {
				switch j.job {
				case jobPinVersion:
					pins++
					if j.params["name"] != pinVersion {
						t.Errorf("pin-version job named %v, want %q", j.params["name"], pinVersion)
					}
					if len(j.requires()) != 0 {
						t.Errorf("pin-version must be a root job, requires %v", j.requires())
					}
				case jobPushRegistries, jobBuildImage, jobPushCatalog:
					gated++
					if !slices.Contains(j.requires(), pinVersion) {
						t.Errorf("%s (%v) does not require %s: %v", j.params["name"], j.job, pinVersion, j.requires())
					}
					if j.job == jobPushCatalog && j.params["attach_workspace"] != true {
						t.Errorf("chart job %s does not attach the workspace", j.params["name"])
					}
				}
			}
			if pins != 1 {
				t.Errorf("want one %s job, found %d:\n%s", jobPinVersion, pins, got)
			}
			if gated == 0 {
				t.Errorf("no image or chart job rendered:\n%s", got)
			}
		})
	}
}

// Test_PinVersionOnlyWithImageOrChart verifies a workflow that builds neither
// an image nor a published chart (a binary, a node library, a template
// repository) renders no pin-version job.
func Test_PinVersionOnlyWithImageOrChart(t *testing.T) {
	cases := map[string]Config{
		"go binary": {
			RepoName: "klausctl",
			Language: gen.LanguageGo,
			Flavours: gen.FlavourSlice{},
		},
		"node library": {
			RepoName:       repoK8sTypes,
			Language:       gen.LanguageNode,
			PackageManager: PackageManagerNPM,
		},
		"template chart": {
			RepoName:      "template-app",
			Flavours:      gen.FlavourSlice{gen.FlavourApp},
			ComponentType: ComponentTypeTemplate,
			Team:          "honeybadger",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := render(t, c)
			if contains(got, pinVersion) {
				t.Errorf("config without an image or a published chart renders %s:\n%s", pinVersion, got)
			}
		})
	}
}
