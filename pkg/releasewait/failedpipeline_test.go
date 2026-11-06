package releasewait

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	registrymock "github.com/giantswarm/devctl/v8/e2e/mock/registry"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// A failed tag pipeline says nothing about what the registry serves. This
// covers both a fresh wait after the failure and publication between polls.
func TestWaitFailedTagPipelineProbesArtifacts(t *testing.T) {
	testCases := []struct {
		name       string
		ciModel    string
		custom     bool
		failsLater bool
		imageFound bool
		chartFound bool
	}{
		{name: "case 0: generated artifacts are available", ciModel: CIModelGenerated, imageFound: true, chartFound: true},
		{name: "case 1: generated chart is missing", ciModel: CIModelGenerated, imageFound: true},
		{name: "case 2: generated image is missing", ciModel: CIModelGenerated, chartFound: true},
		{name: "case 3: generated artifacts are missing", ciModel: CIModelGenerated},
		{name: "case 4: hand-written artifacts are available", ciModel: CIModelHandWritten, imageFound: true, chartFound: true},
		{name: "case 5: hand-written chart is missing", ciModel: CIModelHandWritten, imageFound: true},
		{name: "case 6: custom artifacts are available", ciModel: CIModelGenerated, custom: true, imageFound: true, chartFound: true},
		{name: "case 7: chart appears as the pipeline fails", ciModel: CIModelGenerated, failsLater: true, imageFound: true, chartFound: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			files := []string{"config.yml", "workflows.yml"}
			if tc.ciModel == CIModelHandWritten {
				files = []string{"config.yml"}
			}
			if tc.custom {
				files = append(files, "custom.yml")
			}
			fx := fixture{
				github:   baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, files),
				registry: sequence.Routes{},
			}
			if tc.ciModel == CIModelGenerated {
				fx.entry = generatedEntry(t)
			} else {
				fx.github[repoRoute("/contents/.circleci/config.yml")] = []sequence.Response{{Body: fileContent(".circleci/config.yml", handWrittenConfig)}}
			}
			jobs := []map[string]any{job("push-to-registries-release", "success"), job("push-chart", "success"), job("go-test", "failed")}
			if tc.custom {
				const custom = `workflows:
  build:
    jobs:
    - architect/push-to-registries:
        name: push-extra
        image: giantswarm/extra
`
				fx.github[repoRoute("/contents/.circleci/custom.yml")] = []sequence.Response{{Body: fileContent(".circleci/custom.yml", custom)}}
				jobs = append(jobs, job("push-extra", "success"))
				fx.registry["HEAD /v2/giantswarm/extra/manifests/1.2.3"] = []sequence.Response{{Status: 200}}
			}
			workflows := [][]map[string]any{{wf("w1", "build", "failed", "2026-10-10T10:00:00Z")}}
			if tc.failsLater {
				workflows = append([][]map[string]any{{wf("w1", "build", "running", "2026-10-10T10:00:00Z")}}, workflows...)
			}
			fx.circleci = pipelineRoutes(workflows, map[string][]map[string]any{"w1": jobs})
			if tc.imageFound {
				fx.registry["HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3"] = []sequence.Response{{Status: 200}}
			}
			if tc.chartFound {
				responses := []sequence.Response{{Status: 200}}
				if tc.failsLater {
					responses = append([]sequence.Response{{Status: 404}}, responses...)
				}
				fx.registry["HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3"] = responses
			}

			result, err := run(t, fx)
			assertExit(t, err, agentcli.ExitRed, "build/go-test")
			// Exercise the same reporter as the CLI: stdout must carry both
			// the registry's answer and the nonzero pipeline verdict.
			var stdout bytes.Buffer
			doc := Document{Envelope: agentcli.NewEnvelope(Command, time.Now()), Result: result}
			assertExit(t, agentcli.Report(&stdout, &doc, agentcli.VerdictAvailable, err), agentcli.ExitRed, "build/go-test")
			var printed Document
			if err := json.Unmarshal(stdout.Bytes(), &printed); err != nil {
				t.Fatal(err)
			}
			if printed.Verdict != agentcli.VerdictCIFailed || printed.ExitCode != agentcli.ExitRed {
				t.Errorf("envelope: %+v", printed.Envelope)
			}
			if printed.CIModel != tc.ciModel || printed.Pipeline == nil {
				t.Fatalf("CI model %q, pipeline %+v", printed.CIModel, printed.Pipeline)
			}
			if diff := cmp.Diff([]string{"build/go-test"}, printed.Pipeline.FailedJobs); diff != "" {
				t.Errorf("failed jobs (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]PipelineWorkflow{{Name: "build", Status: "failed"}}, printed.Pipeline.Workflows); diff != "" {
				t.Errorf("workflows (-want +got):\n%s", diff)
			}
			want := map[string]bool{"giantswarm/kserve-controller": tc.imageFound, "charts/giantswarm/kserve": tc.chartFound}
			if tc.custom {
				want["giantswarm/extra"] = true
			}
			got := map[string]Artifact{}
			for _, artifact := range printed.Artifacts {
				_, path, _ := strings.Cut(artifact.Reference, "/")
				path = strings.TrimSuffix(path, ":1.2.3")
				got[path] = artifact
			}
			if len(got) != len(want) || len(printed.Artifacts) != len(want) {
				t.Fatalf("want %d artifacts, got %+v", len(want), printed.Artifacts)
			}
			for path, found := range want {
				artifact := got[path]
				state, digest := StateMissing, ""
				if found {
					state, digest = StateAvailable, registrymock.Digest(registrymock.Manifest(path))
				}
				if artifact.State != state || artifact.Digest != digest {
					t.Errorf("%s: want %s (%s), got %+v", path, state, digest, artifact)
				}
			}
		})
	}
}

// Observing the artifacts must not replace an already-known CI failure
// with a derivation or registry error. The warning records that limitation.
func TestWaitFailedTagPipelineKeepsVerdictOnArtifactError(t *testing.T) {
	testCases := []struct {
		name    string
		derive  bool
		warning string
	}{
		{name: "case 0: registry refuses the read", warning: "HTTP 401"},
		{name: "case 1: hand-written artifact derivation fails", derive: true, warning: "no push job of the tag pipeline names an image"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fx := failedTagPipeline(t, "")
			if tc.derive {
				fx.entry = nil
				fx.github = baseGitHub([]string{"Dockerfile"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml"})
				fx.github[repoRoute("/contents/.circleci/config.yml")] = []sequence.Response{{Body: fileContent(".circleci/config.yml", handWrittenConfig)}}
				fx.circleci = pipelineRoutes([][]map[string]any{{wf("w1", "build", "failed", "2026-10-10T10:00:00Z")}}, map[string][]map[string]any{"w1": {job("go-test", "failed")}})
			} else {
				fx.registry = sequence.Routes{
					"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 401}},
					"GET /v2/giantswarm/kserve-controller/manifests/1.2.3":  {{Status: 401}},
				}
			}
			var warnings []string
			fx.warn = func(message string) { warnings = append(warnings, message) }
			_, err := run(t, fx)
			assertExit(t, err, agentcli.ExitRed, "the tag pipeline of v1.2.3 failed")
			if len(warnings) != 1 || !strings.Contains(warnings[0], tc.warning) {
				t.Errorf("want warning containing %q, got %q", tc.warning, warnings)
			}
		})
	}
}
