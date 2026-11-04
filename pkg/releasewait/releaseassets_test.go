package releasewait

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// Both tags resolve to testSHA. The candidate's successful upload status is
// still the newest status on that commit while the stable pipeline builds.
func releaseAssetsFixture(t *testing.T, generated bool) fixture {
	t.Helper()
	files := []string{"config.yml"}
	if generated {
		files = append(files, "workflows.yml")
	}
	github := baseGitHub([]string{"main.go", "go.mod"}, []string{"zz_generated.auto_release.yaml"}, files)
	github[repoRoute("/git/ref/tags/"+testTag+"-rc.7")] = []sequence.Response{{Body: map[string]any{
		"object": map[string]any{"type": "commit", "sha": testSHA},
	}}}
	github[repoRoute("/contents/.circleci/config.yml")] = []sequence.Response{{Body: fileContent(".circleci/config.yml", "version: 2.1\nworkflows:\n  build:\n    jobs: [go-build, upload-release-assets]\n")}}
	github = statusesRoute(github, [3]string{"ci/circleci: upload-release-assets", "success", "https://circleci.com/gh/giantswarm/kserve/378"})
	github[repoRoute("/releases/tags/"+testTag)] = []sequence.Response{releaseAssetsResponse(true)}
	circleci := pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-10-11T15:01:34Z")}}, map[string][]map[string]any{
		"w1": {job("go-build", "success"), job("upload-release-assets", "success")},
	})
	candidate := map[string]any{"id": "candidate", "number": 11, "vcs": map[string]any{"tag": testTag + "-rc.7", "revision": testSHA}}
	stable := map[string]any{"id": "p1", "number": 12, "vcs": map[string]any{"tag": testTag, "revision": testSHA}}
	// Put the candidate first so matching by revision would select it.
	circleci["GET /api/v2/project/gh/giantswarm/kserve/pipeline"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{candidate, stable}}}}
	circleci["GET /api/v2/project/gh/giantswarm/kserve/job/378"] = []sequence.Response{{Body: map[string]any{"pipeline": map[string]any{"id": "candidate"}}}}
	circleci["GET /api/v2/pipeline/candidate"] = []sequence.Response{{Body: candidate}}
	circleci["GET /api/v2/pipeline/candidate/workflow"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{wf("candidate-workflow", "build", "success", "2026-10-11T14:00:00Z")}}}}
	fx := fixture{github: github, circleci: circleci}
	if generated {
		entry := fieldsFromYAML(t, "- name: kserve\n  componentType: cli\n  gen:\n    flavours: [cli]\n    language: go\n    ci:\n      generate: true\n      releaseWorkflow: auto-release\n")
		fx.entry = &entry
	}
	return fx
}

func releaseAssetsResponse(withAsset bool) sequence.Response {
	assets := []map[string]any{}
	if withAsset {
		assets = append(assets, map[string]any{"name": "tool_linux_amd64.tar.gz", "browser_download_url": "https://example/stable/tool_linux_amd64.tar.gz", "digest": "sha256:stable"})
	}
	return sequence.Response{Body: map[string]any{"tag_name": testTag, "draft": false, "assets": assets}}
}

func TestWaitReleaseAssetsOfTheStableTag(t *testing.T) {
	for _, generated := range []bool{true, false} {
		model := "hand-written"
		if generated {
			model = "generated"
		}
		t.Run(model, func(t *testing.T) {
			testCases := []struct {
				name       string
				change     func(*fixture)
				wantExit   int
				wantWait   time.Duration
				wantCount  int
				wantReason string
			}{
				{
					name: "case 0: candidate green while stable workflow runs",
					change: func(fx *fixture) {
						fx.circleci["GET /api/v2/pipeline/p1/workflow"] = []sequence.Response{
							{Body: map[string]any{"items": []map[string]any{wf("w1", "build", "running", "2026-10-11T15:01:34Z")}}},
							{Body: map[string]any{"items": []map[string]any{wf("w1", "build", "success", "2026-10-11T15:01:34Z")}}},
						}
					},
					wantWait: IntervalFloor, wantCount: 1,
				},
				{
					name: "case 1: green stable waits for its first asset",
					change: func(fx *fixture) {
						fx.github[repoRoute("/releases/tags/"+testTag)] = []sequence.Response{releaseAssetsResponse(false), releaseAssetsResponse(true)}
					},
					wantWait: IntervalFloor, wantCount: 1,
				},
				{
					name: "case 2: green stable never gets an asset",
					change: func(fx *fixture) {
						fx.github[repoRoute("/releases/tags/"+testTag)] = []sequence.Response{releaseAssetsResponse(false)}
					},
					wantExit: agentcli.ExitTimeout, wantWait: 2 * time.Minute,
					wantReason: "upload-release-assets",
				},
				{
					name: "case 3: no upload job permits an empty release",
					change: func(fx *fixture) {
						fx.circleci["GET /api/v2/workflow/w1/job"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{job("go-build", "success")}}}}
						fx.github[repoRoute("/releases/tags/"+testTag)] = []sequence.Response{releaseAssetsResponse(false)}
					},
				},
				{
					name: "case 4: green workflow jobs are not visible yet",
					change: func(fx *fixture) {
						fx.circleci["GET /api/v2/workflow/w1/job"] = append([]sequence.Response{{Status: 404}, {Status: 404}}, fx.circleci["GET /api/v2/workflow/w1/job"]...)
					},
					wantWait: IntervalFloor, wantCount: 1,
				},
				{
					name: "case 5: stable workflow fails despite candidate success",
					change: func(fx *fixture) {
						fx.circleci["GET /api/v2/pipeline/p1/workflow"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{wf("w1", "build", "failed", "2026-10-11T15:01:34Z")}}}}
						fx.circleci["GET /api/v2/workflow/w1/job"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{job("go-build", "failed")}}}}
					},
					wantExit: agentcli.ExitRed, wantReason: "build/go-build",
				},
				{
					name: "case 6: unresolved candidate status before stable pipeline appears",
					change: func(fx *fixture) {
						fx.circleci["GET /api/v2/project/gh/giantswarm/kserve/pipeline"] = append([]sequence.Response{{Body: map[string]any{"items": []map[string]any{}}}}, fx.circleci["GET /api/v2/project/gh/giantswarm/kserve/pipeline"]...)
						delete(fx.circleci, "GET /api/v2/project/gh/giantswarm/kserve/job/378")
					},
					wantWait: IntervalFloor, wantCount: 1,
				},
				{
					name: "case 7: candidate pipeline alone never proves stable",
					change: func(fx *fixture) {
						fx.circleci["GET /api/v2/project/gh/giantswarm/kserve/pipeline"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{}}}}
					},
					wantExit: agentcli.ExitTimeout, wantWait: 2 * time.Minute,
					wantReason: "no CircleCI pipeline",
				},
				{
					name: "case 8: unresolved successful status never proves stable",
					change: func(fx *fixture) {
						fx.circleci["GET /api/v2/project/gh/giantswarm/kserve/pipeline"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{}}}}
						delete(fx.circleci, "GET /api/v2/project/gh/giantswarm/kserve/job/378")
					},
					wantExit: agentcli.ExitTimeout, wantWait: 2 * time.Minute,
					wantReason: "no CircleCI pipeline verified for tag",
				},
				{
					name: "case 9: status resolves a pipeline verified by stable tag",
					change: func(fx *fixture) {
						fx.circleci["GET /api/v2/project/gh/giantswarm/kserve/pipeline"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{}}}}
						fx.github = statusesRoute(fx.github, [3]string{"ci/circleci: upload-release-assets", "success", "https://circleci.com/gh/giantswarm/kserve/381"})
						fx.circleci["GET /api/v2/project/gh/giantswarm/kserve/job/381"] = []sequence.Response{{Body: map[string]any{"pipeline": map[string]any{"id": "p1"}}}}
						fx.circleci["GET /api/v2/pipeline/p1"] = []sequence.Response{{Body: map[string]any{"id": "p1", "number": 12, "vcs": map[string]any{"tag": testTag, "revision": testSHA}}}}
					},
					wantCount: 1,
				},
			}
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					fx := releaseAssetsFixture(t, generated)
					tc.change(&fx)
					start := time.Now()
					clock := agentcli.NewVirtualClock(start)
					fx.clock = &clock
					result, err := run(t, fx)
					assertExit(t, err, tc.wantExit, tc.wantReason)
					if got := clock.Now().Sub(start); got != tc.wantWait {
						t.Errorf("waited %s, want %s", got, tc.wantWait)
					}
					if len(result.Artifacts) != tc.wantCount {
						t.Fatalf("artifacts: %+v, want %d", result.Artifacts, tc.wantCount)
					}
					if tc.wantExit == agentcli.ExitOK {
						if result.Pipeline == nil || result.Pipeline.ID != "p1" || result.Pipeline.Number != 12 || len(result.Pipeline.Unfinished) != 0 {
							t.Errorf("stable pipeline: %+v", result.Pipeline)
						}
						if tc.wantCount > 0 && (result.Artifacts[0].Kind != KindReleaseAsset || result.Artifacts[0].Digest != "sha256:stable" || result.Artifacts[0].State != StateAvailable) {
							t.Errorf("stable asset: %+v", result.Artifacts)
						}
					}
					if tc.wantExit == agentcli.ExitOK || tc.wantReason == uploadReleaseAssetsJob {
						if result.ReleaseAssetCount == nil || *result.ReleaseAssetCount != tc.wantCount {
							t.Errorf("releaseAssetCount: %v, want %d", result.ReleaseAssetCount, tc.wantCount)
						}
						if tc.wantCount > 0 || tc.wantExit == agentcli.ExitTimeout {
							if result.Pipeline == nil || len(result.Pipeline.AssetUploadJobs) != 1 || result.Pipeline.AssetUploadJobs[0] != "build/upload-release-assets" {
								t.Errorf("asset upload jobs: %+v", result.Pipeline)
							}
						}
						data, err := json.Marshal(result)
						if err != nil {
							t.Fatal(err)
						}
						var document struct {
							ReleaseAssetCount *int `json:"releaseAssetCount"`
							Pipeline          struct {
								Number          int64    `json:"number"`
								AssetUploadJobs []string `json:"assetUploadJobs"`
							} `json:"pipeline"`
						}
						if err := json.Unmarshal(data, &document); err != nil {
							t.Fatal(err)
						}
						if document.ReleaseAssetCount == nil || *document.ReleaseAssetCount != tc.wantCount || document.Pipeline.Number != 12 || len(document.Pipeline.AssetUploadJobs) != len(result.Pipeline.AssetUploadJobs) {
							t.Errorf("release-assets document: %s", data)
						}
					}
				})
			}
		})
	}
}

func TestWaitReleaseAssetsCannotUseCommitStatusesAlone(t *testing.T) {
	fx := releaseAssetsFixture(t, true)
	fx.circleCIFromStatuses = true
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitTimeout, "no CircleCI pipeline verified for tag")
	if len(result.Artifacts) != 0 || result.ReleaseAssetCount != nil {
		t.Errorf("unverified stable release: %+v", result)
	}
}

func TestWaitReleaseAssetsRecognizesOrbUploadJob(t *testing.T) {
	fx := releaseAssetsFixture(t, true)
	fx.circleci["GET /api/v2/workflow/w1/job"] = []sequence.Response{{Body: map[string]any{"items": []map[string]any{job("architect/upload-release-assets", "success")}}}}
	fx.github[repoRoute("/releases/tags/"+testTag)] = []sequence.Response{releaseAssetsResponse(false)}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitTimeout, "pipeline 12's build/architect/upload-release-assets")
	if result.ReleaseAssetCount == nil || *result.ReleaseAssetCount != 0 || len(result.Artifacts) != 0 {
		t.Errorf("empty release with an upload job: %+v", result)
	}
}
