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
	staleLogin      bool
	entry           *reposetup.Fields
	version         string
	pr              int
	timeout         time.Duration
	catalog         bool
	catalogLists    bool
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
	reg, err := registrymock.Start(fx.registry, fx.staleLogin)
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
		Owner: testOwner, Repo: testRepo, Version: fx.version, PR: fx.pr, Timeout: fx.timeout, Catalog: fx.catalog,
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

func TestWaitStaleLoginIsToolingNotSlow(t *testing.T) {
	fx := fixture{
		entry:      generatedEntry(t),
		staleLogin: true,
		github:     baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
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
			[][]map[string]any{{wf("w1", "build", "running", "2026-09-21T10:00:00Z")}},
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

func TestWaitPullRequestOnLegacyRepositoryIsNotApplicable(t *testing.T) {
	github := baseGitHub([]string{"Dockerfile"}, []string{"zz_generated.create_release.yaml"}, []string{"config.yml"})
	github[repoRoute("/pulls/7")] = []sequence.Response{{Body: map[string]any{"number": 7, "state": "closed", "merged": true, "merge_commit_sha": testSHA}}}
	_, err := run(t, fixture{github: github, pr: 7})
	assertExit(t, err, agentcli.ExitNotApplicable, "legacy")
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

func TestWaitModelMismatchIsAnError(t *testing.T) {
	fx := fixture{
		entry:  generatedEntry(t),
		github: baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.create_release.yaml"}, []string{"config.yml", "workflows.yml"}),
	}
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitUsage, "auto-release but the workflows")
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
