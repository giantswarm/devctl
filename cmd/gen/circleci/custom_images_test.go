package circleci

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"sigs.k8s.io/yaml"
)

// CircleCI permits workflows.version alongside the named workflows. Metadata
// must not stop generation or hide images pushed by a custom workflow.
func Test_Run_CustomWorkflowVersion(t *testing.T) {
	for _, version := range []string{"2", `"2"`} {
		t.Run(version, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.MkdirAll(".circleci", 0o750); err != nil {
				t.Fatal(err)
			}
			custom := "workflows:\n  version: " + version + `
  release-extra:
    jobs:
      - architect/push-to-registries:
          name: push-extra-image
          image: giantswarm/fixture-extra
`
			if err := os.WriteFile(".circleci/custom.yml", []byte(custom), 0o600); err != nil {
				t.Fatal(err)
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			// This chart stamps a version whose image the custom workflow builds.
			cmd.SetArgs([]string{"--repo-name=fixture", "--flavour=app", "--skip-ats", "--override-chart-app-version=true"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("valid custom workflows metadata prevented generation: %v", err)
			}
			data, err := os.ReadFile(".circleci/workflows.yml")
			if err != nil {
				t.Fatal(err)
			}
			var pipeline struct {
				Workflows map[string]struct {
					Jobs []map[string]struct {
						Name     string `json:"name"`
						PreSteps []struct {
							Run struct {
								Command string `json:"command"`
							} `json:"run"`
						} `json:"pre-steps"`
					} `json:"jobs"`
				} `json:"workflows"`
			}
			if err := yaml.Unmarshal(data, &pipeline); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range pipeline.Workflows["build"].Jobs {
				job := entry["architect/push-to-app-catalog"]
				if job.Name != "build-chart" {
					continue
				}
				for _, step := range job.PreSteps {
					if strings.Contains(step.Run.Command, "ABS_HELM_IMAGE_REFERENCE_VALIDATOR_OWN_IMAGE=gsoci.azurecr.io/giantswarm/fixture-extra") {
						found = true
					}
				}
			}
			if !found {
				t.Error("build-chart must exempt the image built by the custom workflow")
			}
			gotCustom, err := os.ReadFile(".circleci/custom.yml")
			if err != nil || string(gotCustom) != custom {
				t.Fatalf("custom.yml changed: err=%v", err)
			}
		})
	}
}

func Test_Run_CustomWorkflowErrorsLeaveFilesUntouched(t *testing.T) {
	for name, custom := range map[string]string{
		"malformed YAML":     "workflows: [\n",
		"workflow scalar":    "workflows:\n  release-extra: 2\n",
		"workflow jobs map":  "workflows:\n  release-extra:\n    jobs: {}\n",
		"workflows sequence": "workflows: []\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.MkdirAll(".circleci", 0o750); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				".circleci/config.yml":    "previous setup\n",
				".circleci/workflows.yml": "previous workflows\n",
				".circleci/custom.yml":    custom,
			}
			for path, content := range files {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--repo-name=fixture", "--language=go"})
			if err := cmd.Execute(); !IsInvalidConfig(err) || !strings.Contains(err.Error(), ".circleci/custom.yml") {
				t.Fatalf("malformed custom workflows: got %v, want invalid config naming custom.yml", err)
			}
			for path, want := range files {
				got, err := os.ReadFile(path) // #nosec G304 -- fixed fixture paths under t.TempDir, test-only
				if err != nil || string(got) != want {
					t.Errorf("%s changed after failed probe: err=%v", path, err)
				}
			}
		})
	}
}
