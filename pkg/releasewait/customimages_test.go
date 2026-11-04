package releasewait

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// These are the publishing commands and workflow aliases at models
// v0.6.0-rc.6 and llm-d v0.7.0. The image names and tags are computed by
// their tools, so the wait must name the unchecked jobs, not guess images.
const customImagePublishers = `jobs:
  publish-models:
    steps:
    - run:
        command: |
          go build -o /tmp/models .
          /tmp/models publish \
            --registry gsoci.azurecr.io/giantswarm/models
  fast-image:
    steps:
    - when:
        condition: << parameters.push >>
        steps:
        - run: crane push layout "${DEST}"
  mirror-image:
    steps:
    - run: skopeo copy --all --preserve-digests "${SRC}" "${DEST}"
  branch-only:
    steps:
    - run: docker push "$IMAGE"
  test-only:
    steps:
    - run: go test ./...
workflows:
  build:
    jobs:
    - publish-models
    - fast-image:
        name: slim-image-llm-d-cuda
    - mirror-image:
        name: mirror-llm-d-cuda
    - branch-only
    - test-only
`

func TestWaitGeneratedCustomImagePublishers(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pipeline   []map[string]any
		wantJobs   []string
		wantImages []string
	}{
		{
			name:     "models has no derived artifacts",
			pipeline: []map[string]any{job("publish-models", "success")},
			wantJobs: []string{"publish-models"},
		},
		{
			name:       "llm-d includes orb images and aliased custom publishers",
			pipeline:   []map[string]any{job("push-sidecar", "success"), job("slim-image-llm-d-cuda", "success"), job("mirror-llm-d-cuda", "success")},
			wantJobs:   []string{"fast-image", "mirror-image"},
			wantImages: []string{"/giantswarm/router-sidecar:1.2.3"},
		},
		{
			name:     "a job absent from the tag pipeline is not warned about",
			pipeline: []map[string]any{job("test-only", "success")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := fieldsFromYAML(t, `- name: kserve
  gen:
    flavours: [generic]
    language: generic
    ci:
      generate: true
`)
			gh := baseGitHub(nil, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml", "custom.yml"})
			custom := customImagePublishers
			if len(tc.wantImages) > 0 {
				custom += `    - architect/push-to-registries:
        name: push-sidecar
        image: giantswarm/router-sidecar
`
			}
			gh[repoRoute("/contents/.circleci/custom.yml")] = []sequence.Response{{Body: fileContent(".circleci/custom.yml", custom)}}
			gh[repoRoute("/releases/tags/"+testTag)] = []sequence.Response{{Body: map[string]any{"tag_name": testTag, "draft": false, "assets": []any{}}}}
			var warnings []string
			cc := pipelineRoutes([][]map[string]any{
				{wf("w1", "build", "running", "2026-10-11T10:00:00Z")},
				{wf("w1", "build", "success", "2026-10-11T10:00:00Z")},
			}, map[string][]map[string]any{"w1": tc.pipeline})
			// A green workflow can precede its job listing. Do not finish
			// silently before discovering the unchecked publisher.
			cc["GET /api/v2/workflow/w1/job"] = append([]sequence.Response{{Status: 404}}, cc["GET /api/v2/workflow/w1/job"]...)
			result, err := run(t, fixture{
				entry: &entry, github: gh,
				circleci: cc,
				registry: sequence.Routes{"HEAD /v2/giantswarm/router-sidecar/manifests/1.2.3": {{Status: 200}}},
				warn:     func(w string) { warnings = append(warnings, w) },
			})
			assertExit(t, err, agentcli.ExitOK, "")
			if len(warnings) != len(tc.wantJobs) {
				t.Fatalf("want warnings for %v, got %q", tc.wantJobs, warnings)
			}
			for i, name := range tc.wantJobs {
				for _, part := range []string{name, ".circleci/custom.yml", "unchecked", "--image"} {
					if !strings.Contains(warnings[i], part) {
						t.Errorf("warning %q does not name %q", warnings[i], part)
					}
				}
			}
			var refs []string
			for _, a := range result.Artifacts {
				refs = append(refs, a.Reference[strings.Index(a.Reference, "/"):])
				if a.State != StateAvailable || a.Digest == "" {
					t.Errorf("artifact was not probed: %+v", a)
				}
			}
			if !slices.Equal(refs, tc.wantImages) {
				t.Errorf("want %v, got artifacts %v", tc.wantImages, refs)
			}
		})
	}
}

func TestPublishesImage(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{`docker push "$IMAGE"`, true},
		{"docker buildx build \\\n  --platform linux/arm64 \\\n  --push -t \"$IMAGE\" .", true},
		{`docker buildx bake --push`, true},
		{`docker buildx build --push=true .`, true},
		{`docker buildx build --push=false .`, false},
		{`docker buildx build --load .`, false},
		{`crane push layout "${DEST}"`, true},
		{`crane copy "$SRC" "$DEST"`, true},
		{`skopeo copy --all --preserve-digests "${SRC}" "${DEST}"`, true},
		{"/tmp/models publish \\\n  --registry gsoci.azurecr.io/giantswarm/models", true},
		{`models publish --registry=gsoci.azurecr.io/giantswarm/models`, true},
		{`if ready; then docker push "$IMAGE"; fi`, true},
		{`for image in a b; do crane copy "$image" "$DEST"; done`, true},
		{`build && sudo docker push "$IMAGE"`, true},
		{`echo "docker push example/image"`, false},
		{`printf 'crane copy source destination'`, false},
		{`# docker push example/image`, false},
		{`# example; docker push example/image`, false},
		{`go test ./...`, false},
		{`docker build .`, false},
		{`docker pull "$IMAGE"`, false},
		{`crane manifest "$IMAGE"`, false},
		{`skopeo inspect "$IMAGE"`, false},
		{`models validate --registry gsoci.azurecr.io/giantswarm/models`, false},
	} {
		t.Run(tc.command, func(t *testing.T) {
			if got := publishesImage(tc.command); got != tc.want {
				t.Errorf("publishesImage = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestWaitGeneratedCustomTestsDoNotReadJobs(t *testing.T) {
	gh := baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml", "custom.yml"})
	custom := `jobs:
  test-only:
    steps:
    - run: go test ./...
    - run: echo 'docker push example/image'
workflows:
  version: 2
  build:
    jobs: [test-only]
`
	gh[repoRoute("/contents/.circleci/custom.yml")] = []sequence.Response{{Body: fileContent(".circleci/custom.yml", custom)}}
	cc := pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-10-11T10:00:00Z")}}, nil)
	cc["GET /api/v2/workflow/w1/job"] = []sequence.Response{{Status: 404}}
	var warnings []string
	result, err := run(t, fixture{
		entry: generatedEntry(t), github: gh, circleci: cc,
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
		warn: func(w string) { warnings = append(warnings, w) },
	})
	assertExit(t, err, agentcli.ExitOK, "")
	if len(warnings) != 0 || len(result.Artifacts) != 2 {
		t.Errorf("warnings %q, artifacts %+v", warnings, result.Artifacts)
	}
}

func TestWaitGeneratedNamedCustomImage(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			gh := baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"})
			status := 200
			if missing {
				status = 404
			}
			var warnings []string
			result, err := run(t, fixture{
				entry: generatedEntry(t), github: gh,
				circleci: pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-10-11T10:00:00Z")}}, nil),
				registry: sequence.Routes{
					"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
					"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
					"HEAD /v2/giantswarm/extra/manifests/v1.2.3":            {{Status: status}},
				},
				images: []string{"giantswarm/extra"},
				warn:   func(w string) { warnings = append(warnings, w) },
			})
			wantExit, wantState := agentcli.ExitOK, StateAvailable
			if missing {
				wantExit, wantState = agentcli.ExitTimeout, StateMissing
			}
			assertExit(t, err, wantExit, "")
			if len(warnings) != 0 {
				t.Errorf("named image unexpectedly warned as ignored: %q", warnings)
			}
			if len(result.Artifacts) != 3 {
				t.Fatalf("want entry's image and chart plus named image, got %+v", result.Artifacts)
			}
			if a := result.Artifacts[2]; !strings.HasSuffix(a.Reference, "/giantswarm/extra:v1.2.3") || a.State != wantState {
				t.Errorf("named image: %+v", a)
			}
		})
	}
}
