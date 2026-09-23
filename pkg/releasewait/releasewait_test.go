package releasewait

import (
	"context"
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	circlecimock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	registrymock "github.com/giantswarm/devctl/v8/e2e/mock/registry"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const (
	testOwner = "giantswarm"
	testRepo  = "kserve"
	testTag   = "v1.2.3"
	testSHA   = "0123456789abcdef0123456789abcdef01234567"
)

// fixture is one wait against the three mocks.
type fixture struct {
	github          sequence.Routes
	circleci        sequence.Routes
	registry        sequence.Routes
	privateRegistry sequence.Routes
	entry           *reposetup.Fields
	version         string
	pr              int
	mergeCommit     string
	timeout         time.Duration
	catalog         bool
	catalogLists    bool
	// warn collects the document's warnings; nil drops them.
	warn func(string)
}

type entries struct{ fields *reposetup.Fields }

func (e entries) FindEntry(context.Context, string, string) (*reposetup.Fields, bool, error) {
	return e.fields, e.fields != nil, nil
}

type catalog struct{ lists bool }

func (c catalog) Lists(context.Context, string, string, string) (bool, error) { return c.lists, nil }

func dirListing(names ...string) []map[string]any {
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{"name": n, "path": n, "type": "file"})
	}
	return out
}

func fileContent(path, content string) map[string]any {
	return map[string]any{
		"type": "file", "encoding": "base64", "path": path, "name": path[strings.LastIndex(path, "/")+1:],
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	}
}

func repoRoute(path string) string { return "GET /repos/" + testOwner + "/" + testRepo + path }

func discardLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

// baseGitHub scripts what every wait reads: the tag, the repository, the
// tree at the tag with an auto-release workflow.
func baseGitHub(root, workflows, circleci []string) sequence.Routes {
	routes := sequence.Routes{
		repoRoute("/git/ref/tags/" + testTag):    {{Body: map[string]any{"ref": "refs/tags/" + testTag, "object": map[string]any{"type": "commit", "sha": testSHA}}}},
		repoRoute(""):                            {{Body: map[string]any{"name": testRepo, "private": false}}},
		repoRoute("/contents/"):                  {{Body: dirListing(root...)}},
		repoRoute("/contents/.github/workflows"): {{Body: dirListing(workflows...)}},
	}
	if circleci != nil {
		routes[repoRoute("/contents/.circleci")] = []sequence.Response{{Body: dirListing(circleci...)}}
	}
	return routes
}

func generatedEntry(t *testing.T) *reposetup.Fields {
	f := fieldsFromYAML(t, `- name: kserve
  gen:
    flavours: [app]
    language: go
    ci:
      generate: true
      image:
        name: giantswarm/kserve-controller
`)
	return &f
}

func pipelineRoutes(workflowStatuses [][]map[string]any, jobs map[string][]map[string]any) sequence.Routes {
	routes := sequence.Routes{
		"GET /api/v2/project/gh/giantswarm/kserve/pipeline": {{Body: map[string]any{"items": []map[string]any{{"id": "p1", "number": 12, "vcs": map[string]any{"tag": testTag, "revision": testSHA}}}}}},
	}
	var seq []sequence.Response
	for _, items := range workflowStatuses {
		seq = append(seq, sequence.Response{Body: map[string]any{"items": items}})
	}
	routes["GET /api/v2/pipeline/p1/workflow"] = seq
	for id, items := range jobs {
		routes["GET /api/v2/workflow/"+id+"/job"] = []sequence.Response{{Body: map[string]any{"items": items}}}
	}
	return routes
}

func wf(id, name, status, createdAt string) map[string]any {
	return map[string]any{"id": id, "name": name, "status": status, "pipeline_number": 12, "created_at": createdAt}
}

func job(name, status string) map[string]any {
	return map[string]any{"name": name, "status": status, "type": "build"}
}

func run(t *testing.T, fx fixture) (Result, error) {
	t.Helper()
	gh, err := githubmock.Start(fx.github)
	if err != nil {
		t.Fatalf("github mock: %v", err)
	}
	t.Cleanup(gh.Close)
	cc, err := circlecimock.Start(fx.circleci)
	if err != nil {
		t.Fatalf("circleci mock: %v", err)
	}
	t.Cleanup(cc.Close)
	reg, err := registrymock.Start(fx.registry, false)
	if err != nil {
		t.Fatalf("registry mock: %v", err)
	}
	t.Cleanup(reg.Close)
	preg, err := registrymock.Start(fx.privateRegistry, false)
	if err != nil {
		t.Fatalf("private registry mock: %v", err)
	}
	t.Cleanup(preg.Close)

	endpoints := agentcli.Endpoints{
		GitHubAPIURL: gh.URL, CircleCIAPIURL: cc.APIURL(),
		RegistryPublic: reg.Host(), RegistryPrivate: preg.Host(), RegistryInsecure: true,
	}
	ghClient, conditional, err := githubclient.NewConditional(githubclient.Config{Logger: discardLogger(), AccessToken: "ghu_test", BaseURL: gh.URL})
	if err != nil {
		t.Fatalf("github client: %v", err)
	}
	circleCalls := 0
	config := Config{
		Owner: testOwner, Repo: testRepo, Version: fx.version, PR: fx.pr, MergeCommitSHA: fx.mergeCommit, Timeout: fx.timeout, Catalog: fx.catalog,
		GitHub:  ghClient,
		Entries: entries{fx.entry},
		CircleCI: func(context.Context) (CircleCI, error) {
			circleCalls++
			return circleciclient.New(circleciclient.Config{Token: "cci_test", BaseURL: circleciclient.BaseURLFromAPIURL(endpoints.CircleCIAPIURL)})
		},
		Registry:     RegistryProber{Endpoints: endpoints},
		CatalogIndex: catalog{fx.catalogLists},
		Endpoints:    endpoints,
		Clock:        agentcli.NewClock(0.001, nil),
		Rate:         conditional.Rate,
		Warn:         fx.warn,
	}
	if config.Timeout == 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.Version == "" && config.PR == 0 {
		config.Version = testTag
	}
	w, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var result Result
	err = w.Wait(context.Background(), &result)
	if fx.circleci == nil && circleCalls != 0 {
		t.Errorf("CircleCI was opened %d time(s) for a repository without CircleCI", circleCalls)
	}
	return result, err
}

func assertExit(t *testing.T, err error, code int, reason string) {
	t.Helper()
	if agentcli.Exit(err) != code {
		t.Fatalf("want exit %d, got %d (%v)", code, agentcli.Exit(err), err)
	}
	if reason != "" && (err == nil || !strings.Contains(err.Error(), reason)) {
		t.Fatalf("want reason containing %q, got %v", reason, err)
	}
}

func TestWaitGeneratedRenamedImageBecomesAvailable(t *testing.T) {
	fx := fixture{
		entry:  generatedEntry(t),
		github: baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: pipelineRoutes(
			[][]map[string]any{
				{wf("w1", "build", "running", "2026-09-21T10:00:00Z")},
				{wf("w1", "build", "success", "2026-09-21T10:00:00Z")},
			},
			map[string][]map[string]any{"w1": {job("push-to-registries-release", "running"), job("push-chart-release", "running")}},
		),
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 404}, {Status: 200, Headers: map[string]string{"Docker-Content-Digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"}}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.Tag != testTag || result.SHA != testSHA || result.ReleaseModel != ReleaseModelAutoRelease || result.CIModel != CIModelGenerated {
		t.Errorf("head: %+v", result)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("artifacts: %+v", result.Artifacts)
	}
	image, chart := result.Artifacts[0], result.Artifacts[1]
	if !strings.HasSuffix(image.Reference, "/giantswarm/kserve-controller:1.2.3") || image.State != StateAvailable || image.Digest != "sha256:1111111111111111111111111111111111111111111111111111111111111111" {
		t.Errorf("image: %+v", image)
	}
	if !strings.HasSuffix(chart.Reference, "/charts/giantswarm/kserve:1.2.3") || chart.State != StateAvailable || !strings.HasPrefix(chart.Digest, "sha256:") {
		t.Errorf("chart: %+v", chart)
	}
	if result.Pipeline == nil || result.Pipeline.Number != 12 || len(result.Pipeline.Workflows) != 1 || len(result.Pipeline.FailedJobs) != 0 {
		t.Errorf("pipeline: %+v", result.Pipeline)
	}
}

func TestWaitFailedTagPipeline(t *testing.T) {
	fx := fixture{
		entry:  generatedEntry(t),
		github: baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: pipelineRoutes(
			[][]map[string]any{{wf("w1", "build", "failed", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("go-build", "success"), job("push-to-registries-release", "failed"), job("push-chart-release", "blocked")}},
		),
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitRed, "build/push-to-registries-release")
	if result.Pipeline == nil || strings.Join(result.Pipeline.FailedJobs, ",") != "build/push-to-registries-release" {
		t.Errorf("pipeline: %+v", result.Pipeline)
	}
	for _, a := range result.Artifacts {
		if a.State != StateMissing {
			t.Errorf("artifact %s should be missing", a.Reference)
		}
	}
}

func TestWaitRerunReplacesFailedWorkflow(t *testing.T) {
	fx := fixture{
		entry:  generatedEntry(t),
		github: baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: pipelineRoutes(
			[][]map[string]any{{
				wf("w1", "build", "failed", "2026-09-21T10:00:00Z"),
				wf("w2", "build", "success", "2026-09-21T10:30:00Z"),
			}},
			map[string][]map[string]any{"w1": {job("push-to-registries-release", "failed")}, "w2": {job("push-to-registries-release", "success")}},
		),
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if len(result.Pipeline.Workflows) != 1 || result.Pipeline.Workflows[0].Status != "success" {
		t.Errorf("workflows: %+v", result.Pipeline.Workflows)
	}
}

func TestWaitTimeoutNamesTheMissing(t *testing.T) {
	fx := fixture{
		entry:   generatedEntry(t),
		timeout: 45 * time.Second,
		github:  baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: pipelineRoutes(
			[][]map[string]any{{wf("w1", "build", "running", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("push-to-registries-release", "running")}},
		),
		registry: sequence.Routes{
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3": {{Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitTimeout, "image")
	if result.Artifacts[0].State != StateMissing || result.Artifacts[1].State != StateAvailable {
		t.Errorf("artifacts: %+v", result.Artifacts)
	}
}

// giantswarm/vm-manager v0.22.3: the image and the chart the entry names
// resolved while the repository's own guest-image job (custom.yml), which
// pushes the release's third artifact, was still running. The release is
// out when the tag pipeline is green, not when the named artifacts resolve.
func TestWaitRepoOwnedTagJobStillRunningIsNotYet(t *testing.T) {
	circleci := pipelineRoutes(
		[][]map[string]any{
			{wf("w1", "build", "running", "2026-09-23T14:19:18Z")},
			{wf("w1", "build", "running", "2026-09-23T14:19:18Z")},
			{wf("w1", "build", "success", "2026-09-23T14:19:18Z")},
		},
		nil,
	)
	circleci["GET /api/v2/workflow/w1/job"] = []sequence.Response{
		{Body: map[string]any{"items": []map[string]any{job("push-to-registries-release", "success"), job("push-chart-release", "success"), job("guest-image", "running")}}},
		{Body: map[string]any{"items": []map[string]any{job("push-to-registries-release", "success"), job("push-chart-release", "success"), job("guest-image", "running")}}},
		{Body: map[string]any{"items": []map[string]any{job("push-to-registries-release", "success"), job("push-chart-release", "success"), job("guest-image", "success")}}},
	}
	fx := fixture{
		entry:    generatedEntry(t),
		github:   baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml", "custom.yml"}),
		circleci: circleci,
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.Pipeline == nil || len(result.Pipeline.Workflows) != 1 || result.Pipeline.Workflows[0].Status != "success" || len(result.Pipeline.Unfinished) != 0 {
		t.Errorf("the wait ended before the tag pipeline finished: %+v", result.Pipeline)
	}
}

// Artifacts that resolve under a pipeline that never finishes are a timeout
// whose reason says the artifacts are there and what still runs.
func TestWaitArtifactsAvailablePipelineUnfinishedIsTimeout(t *testing.T) {
	fx := fixture{
		entry:   generatedEntry(t),
		timeout: 45 * time.Second,
		github:  baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml", "custom.yml"}),
		circleci: pipelineRoutes(
			[][]map[string]any{{wf("w1", "build", "running", "2026-09-23T14:19:18Z")}},
			map[string][]map[string]any{"w1": {job("push-to-registries-release", "success"), job("push-chart-release", "success"), job("guest-image", "running")}},
		),
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitTimeout, "every artifact of v1.2.3 is available, the tag pipeline did not finish within 45s; pipeline 12 unfinished: build (running)")
	for _, a := range result.Artifacts {
		if a.State != StateAvailable {
			t.Errorf("artifact %s should be available", a.Reference)
		}
	}
}

// A repository's own tag job that fails after the named artifacts resolved
// fails the release: the verdict no longer depends on which came first.
func TestWaitRepoOwnedTagJobFailingAfterTheArtifactsIsCIFailure(t *testing.T) {
	circleci := pipelineRoutes(
		[][]map[string]any{
			{wf("w1", "build", "running", "2026-09-23T14:19:18Z")},
			{wf("w1", "build", "failed", "2026-09-23T14:19:18Z")},
		},
		nil,
	)
	circleci["GET /api/v2/workflow/w1/job"] = []sequence.Response{
		{Body: map[string]any{"items": []map[string]any{job("push-to-registries-release", "success"), job("push-chart-release", "success"), job("guest-image", "running")}}},
		{Body: map[string]any{"items": []map[string]any{job("push-to-registries-release", "success"), job("push-chart-release", "success"), job("guest-image", "failed")}}},
	}
	fx := fixture{
		entry:    generatedEntry(t),
		github:   baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml", "custom.yml"}),
		circleci: circleci,
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitRed, "build/guest-image")
	for _, a := range result.Artifacts {
		if a.State != StateAvailable {
			t.Errorf("artifact %s should be available", a.Reference)
		}
	}
}

// A 401 to the anonymous read of the public registry is the artifact not
// being public, a tooling failure the wait ends with at once, not a slow
// pipeline it waits out.
func TestWaitUnauthorizedIsToolingNotSlow(t *testing.T) {
	fx := fixture{
		entry:  generatedEntry(t),
		github: baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 401}},
		},
		circleci: pipelineRoutes(
			[][]map[string]any{{wf("w1", "build", "running", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("push-to-registries-release", "running")}},
		),
	}
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitUsage, "HTTP 401")
}

func TestWaitHandWrittenDerivesFromThePipelineJobs(t *testing.T) {
	github := baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.create_release.yaml", "zz_generated.create_release_pr.yaml"}, []string{"config.yml"})
	github[repoRoute("/contents/.circleci/config.yml")] = []sequence.Response{{Body: fileContent(".circleci/config.yml", handWrittenConfig)}}
	fx := fixture{
		entry:  nil,
		github: github,
		circleci: pipelineRoutes(
			[][]map[string]any{
				{wf("w1", "build", "running", "2026-09-21T10:00:00Z")},
				{wf("w1", "build", "success", "2026-09-21T10:00:00Z")},
			},
			map[string][]map[string]any{"w1": {job("go-build", "success"), job("push-to-registries-release", "success"), job("push-llmisvc", "success"), job("push-chart", "running")}},
		),
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
		privateRegistry: sequence.Routes{
			"HEAD /v2/giantswarm/llmisvc-controller/manifests/1.2.3": {{Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.ReleaseModel != ReleaseModelLegacy || result.CIModel != CIModelHandWritten {
		t.Errorf("models: %s %s", result.ReleaseModel, result.CIModel)
	}
	var refs []string
	for _, a := range result.Artifacts {
		refs = append(refs, a.Kind+" "+a.Reference[strings.Index(a.Reference, "/"):])
	}
	want := "image /giantswarm/kserve-controller:1.2.3\nimage /giantswarm/llmisvc-controller:1.2.3\nchart /charts/giantswarm/kserve:1.2.3"
	if strings.Join(refs, "\n") != want {
		t.Errorf("artifacts:\n want %s\n got  %s", want, strings.Join(refs, "\n"))
	}
	if !result.Artifacts[1].Private() {
		t.Errorf("the private-only image should be probed in the private registry")
	}
}

func TestWaitHandWrittenDockerfileWithoutPushJobDisagrees(t *testing.T) {
	github := baseGitHub([]string{"Dockerfile"}, []string{"zz_generated.create_release.yaml"}, []string{"config.yml"})
	github[repoRoute("/contents/.circleci/config.yml")] = []sequence.Response{{Body: fileContent(".circleci/config.yml", handWrittenConfig)}}
	fx := fixture{
		github: github,
		circleci: pipelineRoutes(
			[][]map[string]any{{wf("w1", "build", "running", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("go-build", "success"), job("build-image", "success")}},
		),
	}
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitUsage, "sources disagree")
}

func TestWaitReleaseAssetsOnlyWithoutCircleCI(t *testing.T) {
	github := baseGitHub([]string{"main.go", "go.mod"}, []string{"zz_generated.auto_release.yaml", "release.yaml"}, nil)
	github[repoRoute("/actions/runs")] = []sequence.Response{
		{Body: map[string]any{"total_count": 2, "workflow_runs": []map[string]any{
			{"id": 1, "name": "Auto release", "event": "push", "head_branch": "main", "status": "completed", "conclusion": "success", "html_url": "https://example/1"},
			{"id": 2, "name": "Release binaries", "event": "push", "head_branch": testTag, "status": "in_progress", "conclusion": nil, "html_url": "https://example/2"},
		}}},
		{Body: map[string]any{"total_count": 2, "workflow_runs": []map[string]any{
			{"id": 1, "name": "Auto release", "event": "push", "head_branch": "main", "status": "completed", "conclusion": "success", "html_url": "https://example/1"},
			{"id": 2, "name": "Release binaries", "event": "push", "head_branch": testTag, "status": "completed", "conclusion": "success", "html_url": "https://example/2"},
		}}},
	}
	github[repoRoute("/releases/tags/"+testTag)] = []sequence.Response{{Body: map[string]any{
		"tag_name": testTag, "draft": false, "html_url": "https://example/release",
		"assets": []map[string]any{{"name": "tool_linux_amd64.tar.gz", "browser_download_url": "https://example/tool_linux_amd64.tar.gz", "digest": "sha256:abcd"}},
	}}}
	fx := fixture{
		entry: func() *reposetup.Fields {
			f := fieldsFromYAML(t, "- name: kserve\n  gen:\n    flavours: [cli]\n    language: go\n    ci:\n      releaseWorkflow: auto-release\n")
			return &f
		}(),
		github: github,
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.CIModel != CIModelNone || result.Pipeline != nil {
		t.Errorf("expected no CircleCI: %s %+v", result.CIModel, result.Pipeline)
	}
	if len(result.Actions) != 1 || result.Actions[0].Name != "Release binaries" || result.Actions[0].Conclusion != "success" {
		t.Errorf("actions: %+v", result.Actions)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Kind != KindReleaseAsset || result.Artifacts[0].Digest != "sha256:abcd" || result.Artifacts[0].State != StateAvailable {
		t.Errorf("artifacts: %+v", result.Artifacts)
	}
}

func TestWaitActionsRunFailedIsCIFailure(t *testing.T) {
	github := baseGitHub([]string{"main.go"}, []string{"zz_generated.auto_release.yaml"}, nil)
	github[repoRoute("/actions/runs")] = []sequence.Response{{Body: map[string]any{"total_count": 1, "workflow_runs": []map[string]any{
		{"id": 2, "name": "Release binaries", "event": "push", "head_branch": testTag, "status": "completed", "conclusion": "failure", "html_url": "https://example/2"},
	}}}}
	_, err := run(t, fixture{github: github})
	assertExit(t, err, agentcli.ExitRed, "Release binaries")
}

func TestWaitPullRequestOnLegacyRepositoryIsNoRelease(t *testing.T) {
	github := baseGitHub([]string{"Dockerfile"}, []string{"zz_generated.create_release.yaml"}, []string{"config.yml"})
	github[repoRoute("/pulls/7")] = []sequence.Response{{Body: map[string]any{"number": 7, "state": "closed", "merged": true, "merge_commit_sha": testSHA}}}
	_, err := run(t, fixture{github: github, pr: 7})
	assertExit(t, err, agentcli.ExitNotApplicable, "legacy")
	assertNoRelease(t, err)
}

func TestWaitPullRequestNotMergedIsNotApplicable(t *testing.T) {
	github := baseGitHub([]string{"Dockerfile"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"})
	github[repoRoute("/pulls/7")] = []sequence.Response{{Body: map[string]any{"number": 7, "state": "open", "merged": false}}}
	_, err := run(t, fixture{github: github, pr: 7, entry: generatedEntry(t)})
	assertExit(t, err, agentcli.ExitNotApplicable, "not merged")
}

func TestWaitPullRequestResolvesTheTagFromTheMergeCommit(t *testing.T) {
	github := baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"})
	github[repoRoute("/pulls/7")] = []sequence.Response{{Body: map[string]any{"number": 7, "state": "closed", "merged": true, "merge_commit_sha": testSHA}}}
	github[repoRoute("/tags")] = []sequence.Response{
		{Body: []map[string]any{{"name": "v1.2.2", "commit": map[string]any{"sha": "older"}}}},
		{Body: []map[string]any{{"name": testTag, "commit": map[string]any{"sha": testSHA}}, {"name": "v1.2.2", "commit": map[string]any{"sha": "older"}}}},
	}
	github[repoRoute("/actions/runs")] = autoReleaseRuns(autoReleaseRun("in_progress", ""))
	fx := fixture{
		entry: generatedEntry(t), github: github, pr: 7,
		circleci: pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}}, map[string][]map[string]any{"w1": {job("push-to-registries-release", "success")}}),
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.Tag != testTag || result.SHA != testSHA {
		t.Errorf("head: %+v", result)
	}
}

// CircleCI knows the tag pipeline's setup workflow by id before it lists its
// jobs: GET /workflow/{id}/job is 404 for a short while after the pipeline
// is created. That is the tag not built yet, not a tooling failure: the
// next poll reads the jobs and the wait ends available.
func TestWaitJobsNotVisibleYetIsNotYet(t *testing.T) {
	circleci := pipelineRoutes(
		[][]map[string]any{
			{wf("w1", "setup", "running", "2026-09-21T10:00:00Z")},
			{wf("w1", "setup", "success", "2026-09-21T10:00:00Z"), wf("w2", "build", "success", "2026-09-21T10:01:00Z")},
		},
		map[string][]map[string]any{"w2": {job("push-to-registries-release", "success"), job("push-chart-release", "success")}},
	)
	circleci["GET /api/v2/workflow/w1/job"] = []sequence.Response{
		{Status: 404, Body: map[string]any{"message": "Workflow not found"}},
		{Body: map[string]any{"items": []map[string]any{job("setup", "success")}}},
	}
	fx := fixture{
		entry:    generatedEntry(t),
		github:   baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: circleci,
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 404}, {Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 404}, {Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.Pipeline == nil || len(result.Pipeline.Workflows) != 2 || len(result.Pipeline.Unfinished) != 0 {
		t.Errorf("pipeline: %+v", result.Pipeline)
	}
}

// Jobs that never become visible end the wait at the deadline, exit 2, with
// the workflow in the pipeline's unfinished list and in the reason.
func TestWaitJobsNeverVisibleIsTimeout(t *testing.T) {
	circleci := pipelineRoutes([][]map[string]any{{wf("w1", "setup", "running", "2026-09-21T10:00:00Z")}}, nil)
	circleci["GET /api/v2/workflow/w1/job"] = []sequence.Response{{Status: 404, Body: map[string]any{"message": "Workflow not found"}}}
	fx := fixture{
		entry:    generatedEntry(t),
		timeout:  45 * time.Second,
		github:   baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: circleci,
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitTimeout, "pipeline 12 unfinished: setup (running, jobs not visible yet)")
	if result.Pipeline == nil || strings.Join(result.Pipeline.Unfinished, ",") != "setup (running, jobs not visible yet)" {
		t.Errorf("pipeline: %+v", result.Pipeline)
	}
}

// A 404 on the jobs of a finished workflow is not a young pipeline: it stays
// the tooling failure it is.
func TestWaitJobsNotFoundOnAFinishedWorkflowIsTooling(t *testing.T) {
	circleci := pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}}, nil)
	circleci["GET /api/v2/workflow/w1/job"] = []sequence.Response{{Status: 404, Body: map[string]any{"message": "Workflow not found"}}}
	fx := fixture{
		entry:    generatedEntry(t),
		github:   baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: circleci,
	}
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitUsage, "reading the jobs of workflow build")
}

// giantswarm/mcp-toolkit merged its generated pipeline and auto-release
// workflow while its team-file entry (gen without gen.ci) still resolved
// legacy; the declaration followed hours later. The workflows at the merge
// commit decide, --pr resolves the tag, the artifacts are the generator's
// defaults for the entry, and the document warns about the declaration.
func TestWaitDeclarationBehindTheRepositoryWarns(t *testing.T) {
	github := baseGitHub([]string{"Dockerfile", "main.go"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"})
	github[repoRoute("/pulls/96")] = []sequence.Response{{Body: map[string]any{"number": 96, "state": "closed", "merged": true, "merge_commit_sha": testSHA}}}
	github[repoRoute("/tags")] = []sequence.Response{{Body: []map[string]any{{"name": testTag, "commit": map[string]any{"sha": testSHA}}}}}
	var warnings []string
	fx := fixture{
		entry: func() *reposetup.Fields {
			f := fieldsFromYAML(t, "- name: kserve\n  gen:\n    flavours: [generic]\n    language: go\n")
			return &f
		}(),
		github: github, pr: 96,
		circleci: pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("push-to-registries-release", "success")}}),
		registry: sequence.Routes{"HEAD /v2/giantswarm/kserve/manifests/1.2.3": {{Status: 200}}},
		warn:     func(m string) { warnings = append(warnings, m) },
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.Tag != testTag || result.ReleaseModel != ReleaseModelAutoRelease || result.CIModel != CIModelGenerated {
		t.Errorf("head: %+v", result)
	}
	if len(result.Artifacts) != 1 || !strings.HasSuffix(result.Artifacts[0].Reference, "/giantswarm/kserve:1.2.3") || result.Artifacts[0].State != StateAvailable {
		t.Errorf("artifacts: %+v", result.Artifacts)
	}
	want := "declaration says legacy, repository runs auto-release: the team-file entry resolves gen.ci.releaseWorkflow to legacy while the workflows at 01234567 are the auto-release ones (zz_generated.auto_release.yaml); the repository's workflows decide, align the team-file entry in giantswarm/github (gen.ci.generate: true, or gen.ci.releaseWorkflow: auto-release)"
	if len(warnings) != 1 || warnings[0] != want {
		t.Errorf("warnings:\n want %q\n got  %q", want, warnings)
	}
}

// The other direction: the entry declares the generated pipeline, and with
// it auto-release, while the tag still carries the create-release
// workflows. The tag is the legacy flow's; the document warns.
func TestWaitDeclarationAheadOfTheRepositoryWarns(t *testing.T) {
	var warnings []string
	fx := fixture{
		entry:  generatedEntry(t),
		github: baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.create_release.yaml", "zz_generated.create_release_pr.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("push-to-registries-release", "success"), job("push-chart-release", "success")}}),
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
		warn: func(m string) { warnings = append(warnings, m) },
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.ReleaseModel != ReleaseModelLegacy {
		t.Errorf("release model: %s", result.ReleaseModel)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "declaration says auto-release, repository runs legacy") || !strings.Contains(warnings[0], "(zz_generated.create_release.yaml, zz_generated.create_release_pr.yaml)") {
		t.Errorf("warnings: %q", warnings)
	}
}

// A repository no team file declares whose tag carries the generated
// pipeline: the entry names the artifacts of generated CI, so there is
// nothing to wait for by name and the wait says so instead of guessing.
func TestWaitUndeclaredGeneratedCIIsUsage(t *testing.T) {
	fx := fixture{
		github: baseGitHub([]string{"Dockerfile"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("push-to-registries-release", "success")}}),
	}
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitUsage, "no team-file entry declares giantswarm/kserve")
}

func TestWaitCatalogIndexGatesTheVerdict(t *testing.T) {
	base := fixture{
		entry:   generatedEntry(t),
		catalog: true,
		timeout: 45 * time.Second,
		github:  baseGitHub([]string{"helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("push-chart-release", "success")}}),
		registry: sequence.Routes{"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3": {{Status: 200}}},
	}
	_, err := run(t, base)
	assertExit(t, err, agentcli.ExitTimeout, "")

	base.catalogLists = true
	_, err = run(t, base)
	assertExit(t, err, agentcli.ExitOK, "")
}

func TestWaitVersionSpelledWithoutV(t *testing.T) {
	github := baseGitHub([]string{"Dockerfile"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"})
	fx := fixture{
		entry: func() *reposetup.Fields {
			f := fieldsFromYAML(t, "- name: kserve\n  gen:\n    flavours: [cli]\n    language: go\n    ci:\n      generate: true\n")
			return &f
		}(),
		version: "1.2.3",
		github:  github,
		circleci: pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("push-to-registries-release", "success")}}),
		registry: sequence.Routes{"HEAD /v2/giantswarm/kserve/manifests/1.2.3": {{Status: 200}}},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.Tag != testTag {
		t.Errorf("tag: want %s, got %s", testTag, result.Tag)
	}
}

func TestTeamFileEntries(t *testing.T) {
	teamFile := "- name: other\n  gen:\n    flavours: [cli]\n    language: go\n- name: kserve\n  gen:\n    flavours: [app]\n    language: go\n    ci:\n      generate: true\n      image:\n        name: giantswarm/kserve-controller\n"
	gh, err := githubmock.Start(sequence.Routes{
		"GET /repos/giantswarm/github/contents/repositories":             {{Body: dirListing("team-a.yaml", "team-b.yaml")}},
		"GET /repos/giantswarm/github/contents/repositories/team-a.yaml": {{Body: fileContent("repositories/team-a.yaml", "- name: unrelated\n")}},
		"GET /repos/giantswarm/github/contents/repositories/team-b.yaml": {{Body: fileContent("repositories/team-b.yaml", teamFile)}},
	})
	if err != nil {
		t.Fatalf("github mock: %v", err)
	}
	t.Cleanup(gh.Close)
	client, _, err := githubclient.NewConditional(githubclient.Config{Logger: discardLogger(), AccessToken: "ghu_test", BaseURL: gh.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	finder := TeamFileEntries{GitHub: client.GetUnderlyingClient(context.Background())}

	fields, found, err := finder.FindEntry(context.Background(), "giantswarm", "kserve")
	if err != nil || !found {
		t.Fatalf("FindEntry: found=%v err=%v", found, err)
	}
	if fields.Gen == nil || fields.Gen.CI == nil || fields.Gen.CI.Image == nil || fields.Gen.CI.Image.Name != "giantswarm/kserve-controller" {
		t.Errorf("fields: %+v", fields)
	}

	if _, found, err := finder.FindEntry(context.Background(), "giantswarm", "nowhere"); err != nil || found {
		t.Errorf("undeclared repository: found=%v err=%v", found, err)
	}
	requests := len(gh.Requests())
	if _, found, err := finder.FindEntry(context.Background(), "someone-else", "kserve"); err != nil || found || len(gh.Requests()) != requests {
		t.Errorf("another owner: found=%v err=%v requests=%d", found, err, len(gh.Requests())-requests)
	}
}

// autoReleaseRun is the run the push of the merge commit started of the
// generated auto-release workflow.
func autoReleaseRun(status, conclusion string) map[string]any {
	run := map[string]any{
		"id": 901, "name": "Auto-release", "path": ".github/workflows/zz_generated.auto_release.yaml",
		"event": "push", "head_branch": "main", "head_sha": testSHA, "status": status,
		"html_url": "https://github.com/giantswarm/kserve/actions/runs/901",
	}
	if conclusion != "" {
		run["conclusion"] = conclusion
	}
	return run
}

// autoReleaseRuns answers the listing of the merge commit's runs, one
// answer per poll: a CI run of the same push beside the auto-release run.
func autoReleaseRuns(polls ...map[string]any) []sequence.Response {
	responses := make([]sequence.Response, 0, len(polls))
	for _, auto := range polls {
		responses = append(responses, sequence.Response{Body: map[string]any{"total_count": 2, "workflow_runs": []map[string]any{
			{"id": 902, "name": "CI", "path": ".github/workflows/ci.yaml", "event": "push", "head_branch": "main", "head_sha": testSHA, "status": "completed", "conclusion": "success"},
			auto,
		}}})
	}
	return responses
}

func assertNoRelease(t *testing.T, err error) {
	t.Helper()
	if !IsNoRelease(err) {
		t.Fatalf("want no release following the merge, got %v", err)
	}
	if _, verdict := agentcli.Outcome(err); verdict != agentcli.VerdictNoRelease {
		t.Errorf("want verdict %s, got %s", agentcli.VerdictNoRelease, verdict)
	}
}

// mergedWithoutTag is a merge in an auto-release repository with generated
// CI whose tags never include the merge commit.
func mergedWithoutTag(runs []sequence.Response) sequence.Routes {
	github := baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"})
	github[repoRoute("/tags")] = []sequence.Response{{Body: []map[string]any{{"name": "v1.2.2", "commit": map[string]any{"sha": "older"}}}}}
	github[repoRoute("/actions/runs")] = runs
	return github
}

// The auto-release run of the merge commit finished and tagged nothing: the
// commits warrant no release, and the wait says so at once instead of
// waiting for a tag until the timeout.
func TestWaitPullRequestAutoReleaseFinishedWithoutTagIsNoRelease(t *testing.T) {
	fx := fixture{
		entry: generatedEntry(t), pr: 7, mergeCommit: testSHA, timeout: time.Hour,
		github: mergedWithoutTag(autoReleaseRuns(autoReleaseRun("queued", ""), autoReleaseRun("in_progress", ""), autoReleaseRun("completed", "success"))),
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitNotApplicable, "finished without a tag: the commits since the last release warrant none")
	assertNoRelease(t, err)
	if result.SHA != testSHA || result.Tag != "" || result.ReleaseModel != ReleaseModelAutoRelease {
		t.Errorf("result: %+v", result)
	}
}

// The known merge commit is used as is: no pull request is read (the mock
// scripts none, so a read would fail the wait).
func TestWaitPullRequestWithTheMergeCommitReadsNoPullRequest(t *testing.T) {
	fx := fixture{
		entry: generatedEntry(t), pr: 7, mergeCommit: testSHA,
		github: mergedWithoutTag(autoReleaseRuns(autoReleaseRun("completed", "skipped"))),
	}
	_, err := run(t, fx)
	assertNoRelease(t, err)
}

func TestWaitPullRequestAutoReleaseFailedBeforeTaggingIsCIFailure(t *testing.T) {
	fx := fixture{
		entry: generatedEntry(t), pr: 7, mergeCommit: testSHA,
		github: mergedWithoutTag(autoReleaseRuns(autoReleaseRun("completed", "failure"))),
	}
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitRed, "concluded failure before it tagged")
}

// A pending run is cancelled by the next push to the branch: that push's
// tag carries the merge, and --pr cannot name it.
func TestWaitPullRequestAutoReleaseCancelledIsSuperseded(t *testing.T) {
	fx := fixture{
		entry: generatedEntry(t), pr: 7, mergeCommit: testSHA,
		github: mergedWithoutTag(autoReleaseRuns(autoReleaseRun("completed", "cancelled"))),
	}
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitNotApplicable, "a newer push to main supersedes a pending run")
	if IsNoRelease(err) {
		t.Errorf("a superseded run is not a merge without a release: %v", err)
	}
}

// The run tags before its last step (the CircleCI check) and fails there:
// the tag is read after the run finished, and the wait goes on with it.
func TestWaitPullRequestAutoReleaseFailedAfterTaggingGoesOn(t *testing.T) {
	github := mergedWithoutTag(autoReleaseRuns(autoReleaseRun("completed", "failure")))
	github[repoRoute("/tags")] = []sequence.Response{
		{Body: []map[string]any{{"name": "v1.2.2", "commit": map[string]any{"sha": "older"}}}},
		{Body: []map[string]any{{"name": testTag, "commit": map[string]any{"sha": testSHA}}}},
	}
	fx := fixture{
		entry: generatedEntry(t), pr: 7, mergeCommit: testSHA, github: github,
		circleci: pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}}, map[string][]map[string]any{"w1": {job("push-to-registries-release", "success")}}),
		registry: sequence.Routes{
			"HEAD /v2/giantswarm/kserve-controller/manifests/1.2.3": {{Status: 200}},
			"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3":     {{Status: 200}},
		},
	}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitOK, "")
	if result.Tag != testTag {
		t.Errorf("tag: %+v", result)
	}
}

func TestWaitPullRequestTimeoutNamesTheAutoReleaseRun(t *testing.T) {
	fx := fixture{
		entry: generatedEntry(t), pr: 7, mergeCommit: testSHA, timeout: 45 * time.Second,
		github: mergedWithoutTag(autoReleaseRuns(autoReleaseRun("in_progress", ""))),
	}
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitTimeout, "the Auto-release run https://github.com/giantswarm/kserve/actions/runs/901 is in_progress")
}

// A repository no team file declares and whose workflows hold no release
// workflow tags nothing: no release follows its merges.
func TestWaitPullRequestWithoutAnyReleaseWorkflowIsNoRelease(t *testing.T) {
	github := baseGitHub([]string{"main.go"}, []string{"ci.yaml"}, nil)
	_, err := run(t, fixture{github: github, pr: 7, mergeCommit: testSHA})
	assertExit(t, err, agentcli.ExitNotApplicable, "nothing tags the merge commit")
	assertNoRelease(t, err)
}

// An entry that declares auto-release ahead of the repository: no workflow
// at the merge commit tags it, so no release follows the merge.
func TestWaitPullRequestEntryAheadOfTheWorkflowsIsNoRelease(t *testing.T) {
	github := baseGitHub([]string{"main.go"}, []string{"ci.yaml"}, nil)
	entry := fieldsFromYAML(t, "- name: kserve\n  gen:\n    flavours: [cli]\n    language: go\n    ci:\n      releaseWorkflow: auto-release\n")
	_, err := run(t, fixture{github: github, pr: 7, mergeCommit: testSHA, entry: &entry})
	assertExit(t, err, agentcli.ExitNotApplicable, "declares auto-release, but .github/workflows at 01234567 holds no auto-release workflow")
	assertNoRelease(t, err)
}
