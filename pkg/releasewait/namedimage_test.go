package releasewait

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	registrymock "github.com/giantswarm/devctl/v8/e2e/mock/registry"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// The explicit image must accept both tag spellings, including architect's
// bare image tag: https://github.com/giantswarm/devctl/issues/2605.
func TestWaitNamedImageTagSpellings(t *testing.T) {
	testCases := []struct {
		name        string
		gitTag      string
		available   []string
		private     bool
		orb         bool
		derivedOnly bool
		refusedTag  string
		refusal     int
		wantTag     string
		wantExit    int
		wantReason  string
	}{
		{name: "case 0: v-prefixed image", gitTag: "v8.6.0", available: []string{"v8.6.0"}, wantTag: "v8.6.0"},
		{name: "case 1: architect bare image", gitTag: "v8.6.0", available: []string{"8.6.0"}, wantTag: "8.6.0"},
		{name: "case 2: prefer the git spelling when both exist", gitTag: "v8.6.0", available: []string{"v8.6.0", "8.6.0"}, wantTag: "v8.6.0"},
		{name: "case 3: neither spelling exists", gitTag: "v8.6.0", wantExit: agentcli.ExitTimeout, wantReason: "giantswarm/architect:v8.6.0"},
		{name: "case 4: bare git tag and prefixed image", gitTag: "8.6.0", available: []string{"v8.6.0"}, wantTag: "v8.6.0"},
		{name: "case 5: prerelease", gitTag: "v8.6.0-rc.1", available: []string{"8.6.0-rc.1"}, wantTag: "8.6.0-rc.1"},
		{name: "case 6: private image", gitTag: "v8.6.0", available: []string{"8.6.0"}, private: true, wantTag: "8.6.0"},
		{name: "case 7: orb already names the same bare image", gitTag: "v8.6.0", available: []string{"8.6.0"}, orb: true, wantTag: "8.6.0"},
		{name: "case 8: refusal is not a missing tag", gitTag: "v8.6.0", available: []string{"8.6.0"}, refusedTag: "v8.6.0", refusal: 401, wantExit: agentcli.ExitUsage, wantReason: "HTTP 401"},
		{name: "case 9: alternate refusal names the attempted reference", gitTag: "v8.6.0", refusedTag: "8.6.0", refusal: 403, wantExit: agentcli.ExitUsage, wantReason: "giantswarm/architect:8.6.0: the registry answered HTTP 403"},
		{name: "case 10: derived orb tag remains exact", gitTag: "v8.6.0", available: []string{"v8.6.0"}, orb: true, derivedOnly: true, wantExit: agentcli.ExitTimeout, wantReason: "giantswarm/architect:8.6.0"},
		{name: "case 11: available preferred tag skips refused alternate", gitTag: "v8.6.0", available: []string{"v8.6.0"}, refusedTag: "8.6.0", refusal: 403, wantTag: "v8.6.0"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			github := baseGitHub([]string{"Dockerfile"}, []string{"zz_generated.create_release.yaml"}, []string{"config.yml"})
			delete(github, repoRoute("/git/ref/tags/"+testTag))
			github[repoRoute("/git/ref/tags/"+tc.gitTag)] = []sequence.Response{{Body: map[string]any{"ref": "refs/tags/" + tc.gitTag, "object": map[string]any{"type": "commit", "sha": testSHA}}}}
			github[repoRoute("")] = []sequence.Response{{Body: map[string]any{"name": testRepo, "private": tc.private}}}
			config := "workflows:\n  build:\n    jobs:\n    - build-image\n"
			jobName := "build-image"
			if tc.orb {
				config = "workflows:\n  build:\n    jobs:\n    - architect/push-to-registries:\n        image: giantswarm/architect\n"
				jobName = "push-to-registries"
			}
			github[repoRoute("/contents/.circleci/config.yml")] = []sequence.Response{{Body: fileContent(".circleci/config.yml", config)}}
			circleci := pipelineRoutes(
				[][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}},
				map[string][]map[string]any{"w1": {job(jobName, "success")}},
			)
			circleci["GET /api/v2/project/gh/giantswarm/kserve/pipeline"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{{"id": "p1", "number": 12, "vcs": map[string]any{"tag": tc.gitTag, "revision": testSHA}}}}}}
			registry := sequence.Routes{}
			for _, tag := range tc.available {
				registry["HEAD /v2/giantswarm/architect/manifests/"+tag] = []sequence.Response{{Status: 200}}
			}
			if tc.refusedTag != "" {
				for _, method := range []string{"HEAD", "GET"} {
					registry[method+" /v2/giantswarm/architect/manifests/"+tc.refusedTag] = []sequence.Response{{Status: tc.refusal}}
				}
			}
			start := time.Date(2026, time.October, 9, 0, 0, 0, 0, time.UTC)
			clock := agentcli.NewVirtualClock(start)
			fx := fixture{github: github, circleci: circleci, version: tc.gitTag, images: []string{"giantswarm/architect"}, clock: &clock}
			if tc.derivedOnly {
				fx.images = nil
			}
			if tc.private {
				fx.privateRegistry = registry
			} else {
				fx.registry = registry
			}
			result, err := run(t, fx)
			assertExit(t, err, tc.wantExit, tc.wantReason)
			if tc.wantExit != agentcli.ExitOK {
				return
			}
			if !clock.Now().Equal(start) {
				t.Errorf("available image waited %s before succeeding", clock.Now().Sub(start))
			}
			if result.Tag != tc.gitTag || len(result.Artifacts) != 1 {
				t.Fatalf("want git tag %s and one image, got %+v", tc.gitTag, result)
			}
			artifact := result.Artifacts[0]
			wantSuffix := "/giantswarm/architect:" + tc.wantTag
			wantDigest := registrymock.Digest(registrymock.Manifest("giantswarm/architect"))
			if !strings.HasSuffix(artifact.Reference, wantSuffix) || artifact.State != StateAvailable || artifact.Digest != wantDigest || artifact.Private() != tc.private {
				t.Fatalf("want %s available with digest %s, private=%t; got %+v", wantSuffix, wantDigest, tc.private, artifact)
			}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), fmt.Sprintf(`"reference":%q`, artifact.Reference)) {
				t.Fatalf("JSON does not report the resolved image reference: %s", data)
			}
		})
	}
}

type imageProbeFunc func(context.Context, Artifact) (string, bool, error)

func (f imageProbeFunc) Probe(ctx context.Context, artifact Artifact) (string, bool, error) {
	return f(ctx, artifact)
}

func TestNamedImageTransportFailures(t *testing.T) {
	for _, alternate := range []bool{false, true} {
		t.Run(fmt.Sprintf("alternate=%t", alternate), func(t *testing.T) {
			artifact := namedImageArtifact("giantswarm/architect", "v8.6.0", false, testEndpoints)
			primary := artifact.Reference
			other := "public.example/giantswarm/architect:8.6.0"
			var calls []string
			w := Waiter{config: Config{Registry: imageProbeFunc(func(_ context.Context, a Artifact) (string, bool, error) {
				calls = append(calls, a.Reference)
				if alternate && a.Reference == primary {
					return "", false, nil
				}
				return "", false, errors.New("transport failed")
			})}}
			result := Result{Artifacts: []Artifact{artifact}}
			p := &plan{}
			var wantCalls []string
			for i := 0; i < registryFailureLimit; i++ {
				wantCalls = append(wantCalls, primary)
				if alternate {
					wantCalls = append(wantCalls, other)
				}
				err := w.probe(context.Background(), &result, p)
				if i < registryFailureLimit-1 {
					assertExit(t, err, agentcli.ExitOK, "")
				} else {
					assertExit(t, err, agentcli.ExitUsage, "failed 3 times in a row")
					if alternate && !strings.Contains(err.Error(), other) {
						t.Errorf("failure does not name alternate %s: %v", other, err)
					}
				}
			}
			if !slices.Equal(calls, wantCalls) {
				t.Errorf("probe calls: want %q, got %q", wantCalls, calls)
			}
			if result.Artifacts[0].Reference != primary || result.Artifacts[0].State != StateMissing {
				t.Errorf("transport failure changed the missing image: %+v", result.Artifacts)
			}
		})
	}
}
