package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const (
	owner = "giantswarm"
	name  = "sample-service"
	team  = "team-bumblebee"

	entryYAML = `- name: sample-service
  componentType: service
  description: A sample service
  visibility: public
  gen:
    flavours: [app]
    language: go
`
	archivedEntryYAML = entryYAML + "  lifecycle: archived\n"
	deletedEntryYAML  = entryYAML + "  lifecycle: deleted\n"
	// requiredChecksEntryYAML declares the repository's own GitHub Actions
	// gate: required whatever reported.
	requiredChecksEntryYAML = entryYAML + "  requiredChecks: [\"" + ctxValidate + "\"]\n"
	// agentMergeFalseEntryYAML opts the repository out of agent merges: the
	// ruleset has no bypass actor.
	agentMergeFalseEntryYAML = entryYAML + "  agentMerge: false\n"
	// renovateFalseEntryYAML switches Renovate off for the repository.
	renovateFalseEntryYAML = entryYAML + "  renovate: false\n"
	// declaredRulesetEntryYAML declares a ruleset of the repository's own the
	// team keeps beside the engine's.
	declaredRulesetEntryYAML = entryYAML + "  rulesets: [\"protect-giantswarm\"]\n"
	// pruneRulesetsEntryYAML makes the declaration the whole ruleset set:
	// every undeclared active ruleset is deleted.
	pruneRulesetsEntryYAML = declaredRulesetEntryYAML + "  pruneRulesets: true\n"
	// lineGateEntryYAML gates a fork line's direct pushes: the sync's App
	// replaces the line, and no actor lands an unchecked commit on it or on
	// the maintenance line.
	lineGateEntryYAML = entryYAML + "  lineGate:\n    app: 414149\n    requiredChecks: [run-tests, govulncheck]\n    branches: [release-1.3]\n"
	// configurationEntryYAML is a configuration repository: no template, no
	// generated pipeline.
	configurationEntryYAML = `- name: sample-service
  componentType: configuration
  description: Configuration of the sample installations
  visibility: public
  gen:
    flavours: [generic]
    language: generic
    ci:
      generate: false
`
	// actionsOnlyEntryYAML is a repository on generated CI without a CircleCI
	// job: generic, no chart, no image, its generated CI is GitHub Actions
	// only (auto-release).
	actionsOnlyEntryYAML = `- name: sample-service
  componentType: unspecified
  description: A repository released by GitHub Actions
  visibility: private
  gen:
    flavours: [generic]
    language: generic
    ci:
      generate: true
`
	// templateContentEntryYAML is a template repository whose
	// .circleci/config.yml is content for the repositories created from it.
	templateContentEntryYAML = `- name: sample-service
  componentType: template
  description: A template for MCP servers
  visibility: public
  gen:
    flavours: [generic, app]
    language: go
    ci:
      generate: false
      templateContent: true
`
	privateEntryYAML = `- name: sample-service
  componentType: service
  description: A sample service
  visibility: private
  gen:
    flavours: [app]
    language: go
`
	// multiImageEntryYAML declares a multi-image repository: one image per
	// toolchains/<name>/ directory, each released as <name>/vX.Y.Z.
	multiImageEntryYAML = `- name: sample-service
  componentType: service
  description: A sample service
  visibility: public
  gen:
    flavours: [multi-image]
    language: generic
`
	// customerEntryYAML declares a customer repository: the flavour is the
	// profile, the component type the catalog's type.
	customerEntryYAML = `- name: sample-service
  componentType: customer
  description: A customer repository
  visibility: private
  gen:
    flavours: [customer]
    language: generic
    ci:
      generate: false
`
	// appCatalogEntryYAML declares an app catalog: GitHub Pages serves its
	// index from the repository's default branch, master on the older ones.
	appCatalogEntryYAML = `- name: sample-service
  componentType: appcatalog
  description: An app catalog
  visibility: public
  gen:
    flavours: [generic]
    language: generic
    ci:
      generate: false
`
	// forkEntryYAML declares a fork line: the repository carries an upstream
	// release plus the carried patches on the branch named after the
	// organisation, which its entry declares; nothing is generated for it.
	forkEntryYAML = `- name: sample-service
  componentType: service
  description: A fork line
  visibility: public
  defaultBranch: giantswarm
  gen:
    flavours: [fork]
    language: go
    ci:
      generate: false
`

	// templateEntryYAML declares a template repository: other repositories
	// are created from it, its chart lives under a placeholder directory.
	templateEntryYAML = `- name: sample-service
  componentType: template
  description: A template repository
  visibility: public
  gen:
    flavours: [generic, app]
    language: go
`

	// chartNameEntryYAML declares a chart repository whose chart is named
	// otherwise than the repository: gen.ci.chartName names it, as
	// docs-proxy's entry does for helm/docs-proxy-app. An existing
	// repository's declaration: the creation rules name a new repository
	// after its chart.
	chartNameEntryYAML = entryYAML + `    ci:
      generate: true
      chartName: sample-service-app
`

	// scaffoldSubject is the first commit's subject: conventional, so the
	// generated auto-release workflow tags v0.1.0 from it.
	scaffoldSubject = "feat: initial scaffold of sample-service from giantswarm/template"

	ctxGoBuild  = "ci/circleci: go-build"
	ctxSetup    = "ci/circleci: setup"
	ctxDepGraph = "update-go_modules-graph"
	ctxSemantic = "semantic-pull-request / Validate PR title"
	ctxRelease  = "create-release / Gather facts"
	ctxGhost    = "CircleCI Pipeline"
	ctxValidate = "Validate / Repositories YAML"

	// testCodeownersOverride is a CODEOWNERS override with a review rule
	// of its own, as align-files writes it from giantswarm/github.
	testCodeownersOverride = "# generated by giantswarm/github actions - changes will be overwritten\n* @giantswarm/team-bumblebee\n/helm/ @giantswarm/team-bumblebee @giantswarm/team-other\n"

	// testAppID is the devctl App's id the harness configures.
	testAppID int64 = 424242
	// testAlignAppID is the align-files App the review ruleset lets past the review.
	testAlignAppID int64 = 434343
	// testReleaseAppID is the release App the review ruleset lets past the review.
	testReleaseAppID int64 = 454545
)

// harness wires a Runner to the two fakes with a validated entry.
type harness struct {
	t        *testing.T
	gh       *fakeGitHub
	cc       *fakeCircleCI
	runner   *Runner
	baseline Baseline
	entry    reposetup.Entry
	// codeownersOverride is the repository's CODEOWNERS override the runs
	// hand in with the entry; nil: none.
	codeownersOverride []byte
	// unarchived passes the entry as one the change took lifecycle:
	// archived from (Request.Unarchived).
	unarchived bool
}

// newHarness wires the fakes to an entry validated for its creation, the
// default the creation's rendering writes out included.
func newHarness(t *testing.T, yaml string) *harness {
	t.Helper()
	return newHarnessMode(t, yaml, reposetup.ModeCreate)
}

// newHarnessMode is newHarness with the entry validated in mode: an existing
// repository's entry (reposetup.ModeExisting) is free of the creation rules,
// gen.ci.chartName equal to the repository's name among them.
func newHarnessMode(t *testing.T, yaml string, mode reposetup.Mode) *harness {
	t.Helper()
	ctx := context.Background()
	gh, cc := newFakeGitHub(), newFakeCircleCI()
	t.Cleanup(gh.srv.Close)
	t.Cleanup(cc.srv.Close)

	// The clients count their requests as the CLI's do: one counter under
	// both GitHub clients, one under CircleCI.
	githubRequests, circleciRequests := &Counter{}, &Counter{}
	ghClient, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: githubRequests}), github.WithEnterpriseURLs(gh.srv.URL, gh.srv.URL))
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	checks, err := githubclient.New(githubclient.Config{Logger: logger, AccessToken: "token", BaseURL: gh.srv.URL, Transport: githubRequests})
	require.NoError(t, err)

	ccClient, err := circleciclient.New(circleciclient.Config{Token: "token", BaseURL: cc.srv.URL, Transport: circleciRequests})
	require.NoError(t, err)

	schema, err := reposetup.EmbeddedSchema()
	require.NoError(t, err)
	tf, err := reposetup.ParseTeamFile(team, strings.NewReader(yaml))
	require.NoError(t, err)
	validated, err := reposetup.Validator{Schema: schema}.Validate(ctx, reposetup.Request{TeamFile: tf, Mode: mode})
	require.NoError(t, err)
	require.True(t, validated.Entries[0].Accepted, "%v", validated.Entries[0].Problems)

	h := &harness{t: t, gh: gh, cc: cc, baseline: DefaultBaseline(), entry: validated.Entries[0]}
	h.runner = &Runner{GitHub: ghClient, Checks: checks, CircleCI: ccClient, Renderer: fakeRenderer{}, Baseline: &h.baseline, DevctlAppID: testAppID, GitHubRequests: githubRequests, CircleCIRequests: circleciRequests}
	return h
}

func (h *harness) run(mode Mode, added bool, steps ...Step) *Result {
	h.t.Helper()
	res, err := h.runner.Run(context.Background(), Request{Team: team, Entry: h.entry, Added: added, Unarchived: h.unarchived, Mode: mode, Steps: steps, CodeownersOverride: h.codeownersOverride})
	require.NoError(h.t, err)
	return res
}

// dispatchAsRun gives the runner the workflow run's own token for the
// catalog step's dispatches, the reconciler's set-up: the run token holds
// the Actions permission the default identity lacks and sees public
// repositories only (fakeGitHub.runToken).
func (h *harness) dispatchAsRun() {
	h.t.Helper()
	h.gh.runToken = "run-token"
	client, err := github.NewClient(github.WithAuthToken(h.gh.runToken), github.WithEnterpriseURLs(h.gh.srv.URL, h.gh.srv.URL))
	require.NoError(h.t, err)
	h.runner.Dispatch = client
}

func (h *harness) mutations() []string {
	return append(append([]string{}, h.gh.mutations...), h.cc.mutations...)
}

func (h *harness) resetMutations() {
	h.gh.mutations, h.cc.mutations = nil, nil
}

// gets is the path of every GET the fake GitHub served so far, in order.
func (h *harness) gets() []string {
	h.gh.mu.Lock()
	defer h.gh.mu.Unlock()
	return append([]string{}, h.gh.gets...)
}

func (h *harness) repo() *fakeRepo {
	r, ok := h.gh.repos[owner+"/"+name]
	require.True(h.t, ok, "repository %s/%s not in the fake", owner, name)
	return r
}

// seedCatalog seeds giantswarm/github's catalog and management-cluster-bases'
// mapping, with or without the repository, and makes a dispatched workflow
// land what its run lands: the catalog run adds the component and the
// mapping (which follows the catalog push), the mapping run the mapping.
func (h *harness) seedCatalog(inCatalog, inMapping bool) {
	h.seedCatalogCharts(inCatalog, inMapping, []string{name})
}

// seedCatalogCharts is seedCatalog with the repository's component annotated
// with charts (public unless the reference says otherwise) — none for a
// repository without a chart.
func (h *harness) seedCatalogCharts(inCatalog, inMapping bool, charts []string) {
	catalog := h.gh.addRepo(owner, "github")
	catalog.files = map[string]string{"catalog/components.yaml": componentYAML("other-service", "other-service")}
	mapping := h.gh.addRepo(owner, "management-cluster-bases")
	mapping.files = map[string]string{"bases/apps-to-teams-mapping/configmap.yaml": "data:\n  other-service: rocket\n"}
	mapCharts := func() {
		for _, chart := range charts {
			mapping.files["bases/apps-to-teams-mapping/configmap.yaml"] += "  " + chartName(chart) + ": bumblebee\n"
		}
	}
	if inCatalog {
		catalog.files["catalog/components.yaml"] += componentYAML(name, charts...)
	}
	if inMapping {
		mapCharts()
	}
	h.gh.onDispatch = func(workflow string, _ map[string]any) {
		if workflow == h.baseline.CatalogWorkflow {
			catalog.files["catalog/components.yaml"] += componentYAML(name, charts...)
		}
		mapCharts()
	}
}

// componentYAML is a catalog Component; charts are chart names or full
// registry references (a private one: gsociprivate.azurecr.io/…).
func componentYAML(n string, charts ...string) string {
	return taggedComponentYAML(n, []string{"helmchart", "helmchart-deployable"}, charts...)
}

// taggedComponentYAML is componentYAML with the component's tags, as the
// catalog generator writes them for a chart (private for a private repository).
func taggedComponentYAML(n string, tags []string, charts ...string) string {
	doc := "---\napiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n    name: " + n + "\n"
	if len(charts) == 0 {
		return doc
	}
	doc += "    tags:\n"
	for _, tag := range tags {
		doc += "        - " + tag + "\n"
	}
	refs := make([]string, 0, len(charts))
	for _, c := range charts {
		if !strings.Contains(c, "/") {
			c = "gsoci.azurecr.io/charts/giantswarm/" + c
		}
		refs = append(refs, c)
	}
	return doc + "    annotations:\n        giantswarm.io/helmcharts: " + strings.Join(refs, ",") + "\n"
}

func chartName(ref string) string { return ref[strings.LastIndexByte(ref, '/')+1:] }

type stepCase struct {
	name  string
	entry string
	added bool
	// unarchived passes the entry as one the change took lifecycle:
	// archived from.
	unarchived bool
	// existing validates the entry as an existing repository's
	// (reposetup.ModeExisting), free of the creation rules.
	existing bool
	step     Step
	seed     func(h *harness)
	// wantCheck is the verdict of the check run; wantChange a substring of
	// its changes, wantFinding a finding kind it carries.
	wantCheck   Verdict
	wantChange  string
	wantFinding FindingKind
	// wantRepair is the verdict of the repair run; empty means repaired
	// after a drift and the check's verdict otherwise.
	wantRepair Verdict
	// wantAfter is the verdict of the second repair run; empty means ok.
	wantAfter Verdict
	// verify inspects the fakes after the repair.
	verify func(t *testing.T, h *harness, repair *Result)
}

// seedMetadata adds the repository as the entry declares its description and
// visibility, carrying the topics given.
func seedMetadata(h *harness, topics []string) {
	r := h.gh.addRepo(owner, name)
	r.description, r.private, r.topics = "A sample service", false, topics
}

func TestSteps(t *testing.T) {
	tekton := Webhook{URL: "https://github-pr-webhook.ci.giantswarm.io", Events: []string{"issue_comment", "pull_request", "check_run"}, ContentType: "json", Secret: "shared"}

	cases := []stepCase{
		{
			name: "create: an added entry creates the repository", step: StepCreate, added: true,
			wantCheck: VerdictDrift, wantChange: "create giantswarm/sample-service",
			verify: func(t *testing.T, h *harness, _ *Result) {
				r := h.repo()
				require.Equal(t, "A sample service", r.description)
				require.False(t, r.private)
				require.Equal(t, map[string]string{"README.md": "# sample-service\n"}, r.files, "auto_init leaves the initial README for the scaffold step")
			},
		},
		{
			name: "create: a missing repository is reported, never created from a schedule", step: StepCreate,
			wantCheck: VerdictReported, wantFinding: FindingRepositoryMissing, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, _ *Result) {
				_, ok := h.gh.repos[owner+"/"+name]
				require.False(t, ok)
			},
		},
		{
			name: "create: a redirect on the declared name is a rename", step: StepCreate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name+"-v2")
				h.gh.redirects[owner+"/"+name] = owner + "/" + name + "-v2"
			},
			wantCheck: VerdictReported, wantFinding: FindingRenamed, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, owner+"/"+name+"-v2", res.Repository)
				require.Equal(t, owner+"/"+name, res.Declared)
				require.Contains(t, res.Findings()[0].Fix, `renaming the entry "sample-service" to "sample-service-v2"`)
			},
		},
		{
			name: "scaffold: pushed over the initial commit", step: StepScaffold,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files = map[string]string{"README.md": "# sample-service\n"}
			},
			wantCheck: VerdictDrift, wantChange: "render the scaffold with the chart of giantswarm/template-app at helm/sample-service and push it as the first commit on main",
			wantAfter: VerdictReported, // the default icon
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, scaffoldFiles, h.repo().files)
				require.Equal(t, scaffoldSubject, h.repo().headSubject("main"), "auto-release tags v0.1.0 from a conventional first commit")
				require.Equal(t, []FindingKind{FindingDefaultIcon}, kinds(res.Step(StepScaffold).Findings))
			},
		},
		{
			name: "scaffold: pushed into an empty repository", step: StepScaffold,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.empty, r.files = true, map[string]string{}
			},
			wantCheck: VerdictDrift, wantChange: "render the scaffold", wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, scaffoldFiles, h.repo().files)
				require.Equal(t, scaffoldSubject, h.repo().headSubject("main"), "the initial README's commit is replaced, not built on")
				require.False(t, h.repo().empty)
			},
		},
		{
			name: "scaffold: the CircleCI generator's refusal is reported with the field to set", step: StepScaffold,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files = map[string]string{"README.md": "# sample-service\n"}
				h.runner.Renderer = fakeRenderer{fail: errors.New("gen circleci: no jobs would be generated: set --language=go or --language=node, add a Dockerfile, or use the app flavour")}
			},
			wantCheck: VerdictDrift, wantRepair: VerdictReported, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				f := res.Step(StepScaffold).Findings
				require.Equal(t, []FindingKind{FindingGenCircleCIRefused}, kinds(f))
				require.Contains(t, f[0].Fix, "set gen.language to go or node, add a root Dockerfile")
				require.Len(t, h.repo().files, 1, "nothing pushed")
			},
		},
		{
			name: "scaffold: the default icon alone is advisory, the run converges", step: StepScaffold,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files = maps.Clone(scaffoldFiles)
			},
			wantCheck: VerdictReported, wantFinding: FindingDefaultIcon, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.True(t, res.Converged, "%+v", res.Steps)
				findings := res.Step(StepScaffold).Findings
				require.Equal(t, []FindingKind{FindingDefaultIcon}, kinds(findings))
				require.True(t, findings[0].Advisory)
				data, err := json.Marshal(res)
				require.NoError(t, err)
				require.Contains(t, string(data), `"converged":true`)
				require.Contains(t, string(data), `"fix":"replace icon in Chart.yaml with the application's own icon","advisory":true`)
			},
		},
		{
			name: "scaffold: a chart without an icon is a finding to fix, the run does not converge", step: StepScaffold,
			seed: func(h *harness) {
				files := maps.Clone(scaffoldFiles)
				files["helm/sample-service/Chart.yaml"] = strings.Replace(files["helm/sample-service/Chart.yaml"], "icon: "+defaultChartIcon+"\n", "", 1)
				h.gh.addRepo(owner, name).files = files
			},
			wantCheck: VerdictReported, wantFinding: FindingABSPrerequisite, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.False(t, res.Converged, "%+v", res.Steps)
				require.Equal(t, []FindingKind{FindingABSPrerequisite}, kinds(res.Step(StepScaffold).Findings))
				require.False(t, res.Step(StepScaffold).Findings[0].Advisory)
				data, err := json.Marshal(res)
				require.NoError(t, err)
				require.NotContains(t, string(data), `"advisory"`, "omitted when false")
			},
		},
		{
			name: "scaffold: the chart of a template carries placeholders and is not checked", step: StepScaffold,
			entry: templateEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files = map[string]string{
					"README.md":                  "# sample-service\n",
					"helm/{APP-NAME}/Chart.yaml": "apiVersion: v2\nname: \"{APP-NAME}\"\nversion: 0.0.0\nannotations:\n  io.giantswarm.application.team: \"{TEAM-NAME}\"\n",
				}
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.True(t, res.Converged, "%+v", res.Steps)
				require.Empty(t, res.Step(StepScaffold).Findings, "no chart is expected at helm/sample-service")
				require.Contains(t, res.Step(StepScaffold).Summary, "the chart of a template carries placeholders")
			},
		},
		{
			name: "scaffold: the chart is read at gen.ci.chartName", step: StepScaffold, entry: chartNameEntryYAML, existing: true,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files = chartFilesAt("sample-service-app")
			},
			wantCheck: VerdictReported, wantFinding: FindingDefaultIcon, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.True(t, res.Converged, "%+v", res.Steps)
				findings := res.Step(StepScaffold).Findings
				require.Equal(t, []FindingKind{FindingDefaultIcon}, kinds(findings), "the chart at helm/sample-service-app is the one checked")
				require.Contains(t, findings[0].Message, "helm/sample-service-app/Chart.yaml")
			},
		},
		{
			name: "scaffold: the older team annotation satisfies C0001 as app-build-suite reads it", step: StepScaffold, existing: true,
			seed: func(h *harness) {
				files := maps.Clone(scaffoldFiles)
				files["helm/sample-service/Chart.yaml"] = strings.Replace(files["helm/sample-service/Chart.yaml"],
					"io.giantswarm.application.team: bumblebee", "application.giantswarm.io/team: bumblebee", 1)
				h.gh.addRepo(owner, name).files = files
			},
			wantCheck: VerdictReported, wantFinding: FindingDefaultIcon, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.True(t, res.Converged, "%+v", res.Steps)
				require.Equal(t, []FindingKind{FindingDefaultIcon}, kinds(res.Step(StepScaffold).Findings), "application.giantswarm.io/team is a team label")
			},
		},
		{
			name: "scaffold: a chart without either team annotation is an app-build-suite prerequisite", step: StepScaffold, existing: true,
			seed: func(h *harness) {
				files := maps.Clone(scaffoldFiles)
				files["helm/sample-service/Chart.yaml"] = strings.Replace(files["helm/sample-service/Chart.yaml"],
					"annotations:\n  io.giantswarm.application.team: bumblebee\n", "", 1)
				h.gh.addRepo(owner, name).files = files
			},
			wantCheck: VerdictReported, wantFinding: FindingABSPrerequisite, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.False(t, res.Converged)
				f := res.Step(StepScaffold).Findings
				require.Equal(t, []FindingKind{FindingABSPrerequisite, FindingDefaultIcon}, kinds(f))
				require.Equal(t, "helm/sample-service/Chart.yaml lacks the io.giantswarm.application.team annotation (app-build-suite C0001 HasTeamLabel)", f[0].Message)
				require.Equal(t, "add annotations.io.giantswarm.application.team: bumblebee to Chart.yaml", f[0].Fix)
			},
		},
		{
			name: "scaffold: a chart missing at gen.ci.chartName names the chart the repository has", step: StepScaffold, entry: chartNameEntryYAML, existing: true,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files = maps.Clone(scaffoldFiles)
			},
			wantCheck: VerdictReported, wantFinding: FindingABSPrerequisite, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				f := res.Step(StepScaffold).Findings
				require.Equal(t, []FindingKind{FindingABSPrerequisite}, kinds(f))
				require.Equal(t, "giantswarm/sample-service has no chart at helm/sample-service-app/Chart.yaml", f[0].Message)
				require.Equal(t, "the chart is helm/sample-service: set gen.ci.chartName: sample-service on the entry in repositories/team-bumblebee.yaml, or rename the chart directory and its name to sample-service-app", f[0].Fix)
			},
		},
		{
			name: "scaffold: a renamed repository that kept its chart is told the chartName remedy", step: StepScaffold,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name+"-v2").files = maps.Clone(scaffoldFiles)
				h.gh.redirects[owner+"/"+name] = owner + "/" + name + "-v2"
			},
			wantCheck: VerdictReported, wantFinding: FindingABSPrerequisite, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, owner+"/"+name+"-v2", res.Repository)
				f := res.Step(StepScaffold).Findings
				require.Equal(t, []FindingKind{FindingABSPrerequisite}, kinds(f))
				require.Equal(t, "giantswarm/sample-service-v2 has no chart at helm/sample-service-v2/Chart.yaml", f[0].Message)
				require.Equal(t, "the chart is helm/sample-service: set gen.ci.chartName: sample-service on the entry in repositories/team-bumblebee.yaml, or rename the chart directory and its name to sample-service-v2", f[0].Fix)
			},
		},
		{
			name: "scaffold: a chart repository without any chart is told where the app flavour builds it", step: StepScaffold,
			seed: func(h *harness) {
				files := maps.Clone(scaffoldFiles)
				delete(files, "helm/sample-service/Chart.yaml")
				delete(files, "helm/sample-service/values.schema.json")
				h.gh.addRepo(owner, name).files = files
			},
			wantCheck: VerdictReported, wantFinding: FindingABSPrerequisite, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				f := res.Step(StepScaffold).Findings
				require.Equal(t, []FindingKind{FindingABSPrerequisite}, kinds(f))
				require.Equal(t, "add the chart under helm/sample-service (the app flavour builds it), set gen.ci.chartName when the chart is under another helm/ directory, or drop the app flavour from the entry", f[0].Fix)
			},
		},
		{
			name: "settings: drift from the baseline", step: StepSettings,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.hasWiki, r.allowSquash, r.allowAuto, r.workflowPerm = true, false, false, "read"
				r.squashTitle = "COMMIT_OR_PR_TITLE"
			},
			wantCheck: VerdictDrift, wantChange: "has_wiki true → false, allow_squash_merge false → true, allow_auto_merge false → true, squash_merge_commit_title COMMIT_OR_PR_TITLE → PR_TITLE",
			verify: func(t *testing.T, h *harness, res *Result) {
				r := h.repo()
				require.False(t, r.hasWiki)
				require.True(t, r.allowSquash && r.allowAuto)
				require.Equal(t, "PR_TITLE", r.squashTitle)
				require.Equal(t, "write", r.workflowPerm)
				require.Contains(t, strings.Join(res.Step(StepSettings).Changes, ";"), `default workflow permissions "read" → "write"`)
			},
		},
		{
			name: "permissions: the baseline's teams", step: StepPermissions,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).teams = map[string]string{"employees": "push", "team-rocket": "pull"}
			},
			wantCheck: VerdictDrift, wantChange: "team bots: push; team employees: admin",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, map[string]string{"employees": "admin", "bots": "push", "team-rocket": "pull"}, h.repo().teams, "other teams keep their access")
			},
		},
		{
			name: "protection: a fresh repository requires only what reported and the pipeline has", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild, ctxSetup, ctxDepGraph}
				r.checkRuns = []string{ctxSemantic, ctxRelease}
			},
			wantCheck: VerdictDrift, wantChange: `create ruleset "devctl: default branch"; require ` + ctxSemantic + ", " + ctxGoBuild + "; bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests",
			verify: func(t *testing.T, h *harness, res *Result) {
				rs := h.repo().ruleset("devctl: default branch")
				require.NotNil(t, rs)
				require.Equal(t, github.RulesetEnforcementActive, rs.Enforcement)
				require.Equal(t, github.RulesetTargetBranch, *rs.GetTarget())
				require.Equal(t, []string{"~DEFAULT_BRANCH"}, rs.Conditions.RefName.Include, "the ruleset follows the default branch")
				require.Equal(t, []string{ctxSemantic, ctxGoBuild}, checkContexts(rs))
				checks := rs.Rules.RequiredStatusChecks.RequiredStatusChecks
				require.Equal(t, int64(15368), checks[0].GetIntegrationID(), "a GitHub Actions gate is pinned to the GitHub Actions App")
				require.Nil(t, checks[1].IntegrationID, "a CircleCI status is not pinned")
				require.False(t, rs.Rules.RequiredStatusChecks.StrictRequiredStatusChecksPolicy, "strict checks are off in the baseline")
				require.Equal(t, 1, rs.Rules.PullRequest.RequiredApprovingReviewCount)
				require.False(t, rs.Rules.PullRequest.DismissStaleReviewsOnPush)
				require.NotNil(t, rs.Rules.Deletion)
				require.NotNil(t, rs.Rules.NonFastForward)
				require.Equal(t, []*github.BypassActor{appBypass(testAppID), adminBypass(), teamBypass(testTeamID)}, rs.BypassActors, "the App, the repository admins and the owning team, all on pull requests")
				require.Nil(t, h.repo().protection, "no classic protection is written")
				require.Len(t, res.Step(StepProtection).Changes, 1, "one write: the ruleset")
			},
		},
		{
			name: "protection: what reported on the merged pull requests is required, tag-only jobs are not", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild, ctxSetup}
				r.checkRuns = []string{"pre-commit", ctxRelease}
			},
			wantCheck: VerdictDrift, wantChange: "require pre-commit, " + ctxGoBuild,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{"pre-commit", ctxGoBuild}, checkContexts(h.repo().ruleset(RulesetName)))
				require.Empty(t, res.Step(StepProtection).Findings)
			},
		},
		{
			// The generated values-schema gate of a chart repository runs on
			// every pull request: required once it has reported, as the
			// other generated gates.
			name: "protection: the reported values-schema gate is required", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.checkRuns = []string{"check-values-schema / validate"}
			},
			wantCheck: VerdictDrift, wantChange: "require check-values-schema / validate",
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{"check-values-schema / validate"}, checkContexts(h.repo().ruleset(RulesetName)))
			},
		},
		{
			// Nothing has been merged: nothing has reported, and nothing is
			// required or removed on a guess.
			name: "protection: a repository without a merged pull request keeps its checks", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.merged = nil
				r.statuses = []string{ctxGoBuild}
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: false, checks: []string{"execute-smoke-test"}}
			},
			wantCheck: VerdictDrift, wantChange: `create ruleset "devctl: default branch"`,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{"execute-smoke-test"}, checkContexts(h.repo().ruleset(RulesetName)), "the current checks carry over, nothing is added")
				require.Empty(t, res.Step(StepProtection).Findings, "no merged pull request is not a permission gap")
			},
		},
		{
			name: "protection: classic protection gives way to the ruleset", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: false, checks: []string{ctxGoBuild}}
			},
			wantCheck: VerdictDrift, wantChange: "remove classic protection of main",
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{`create ruleset "devctl: default branch"; bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests`, "remove classic protection of main"}, res.Step(StepProtection).Changes, "the checks carry over, so the ruleset differs from the classic protection in the bypass actors alone")
				require.Nil(t, h.repo().protection)
				require.Equal(t, []string{ctxGoBuild}, checkContexts(h.repo().ruleset(RulesetName)))
			},
		},
		{
			// The discovery of the reported checks is one page of the merged
			// pull requests and the statuses and check runs of the newest
			// head: three requests, once per run, whatever else runs; the
			// branch's tags are not read.
			name: "protection: the reported checks cost three requests per run", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				for i := 1; i <= 3; i++ {
					pr := *r.merged[0]
					pr.Number, pr.Head = new(i+1), &github.PullRequestBranch{SHA: new(fmt.Sprintf("merged-%d", i)), Ref: new(fmt.Sprintf("merged-%d", i))}
					r.merged = append(r.merged, &pr)
				}
				r.statuses = []string{ctxGoBuild, ctxSetup}
				r.checkRuns = []string{"pre-commit", ctxRelease}
			},
			wantCheck: VerdictDrift, wantChange: "require pre-commit, " + ctxGoBuild,
			verify: func(t *testing.T, h *harness, _ *Result) {
				prefix := "/repos/" + owner + "/" + name
				before := len(h.gets())
				h.run(ModeCheck, false) // every step
				var discovery []string
				for _, p := range h.gets()[before:] {
					if p == prefix+"/pulls" || strings.HasSuffix(p, "/status") || strings.HasSuffix(p, "/check-runs") || p == prefix+"/tags" {
						discovery = append(discovery, strings.TrimPrefix(p, prefix))
					}
				}
				require.Equal(t, []string{"/pulls", "/commits/merged-head/status", "/commits/merged-head/check-runs"}, discovery)
			},
		},
		{
			name: "protection: reports the token cannot read are a permission gap, nothing is removed", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.checksStatus = 403
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: false, checks: []string{ctxGoBuild, "execute-smoke-test"}}
			},
			wantCheck: VerdictDrift, wantFinding: FindingUnchecked, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{ctxGoBuild, "execute-smoke-test"}, checkContexts(h.repo().ruleset(RulesetName)), "the classic checks carry over")
				require.Contains(t, res.Step(StepProtection).Findings[0].Fix, "statuses: read, checks: read")
			},
		},
		{
			name: "protection: ghost contexts are removed", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild, ctxSetup, ctxDepGraph}
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: false, checks: []string{ctxGoBuild, ctxSetup, ctxDepGraph, ctxRelease, ctxGhost}}
			},
			wantCheck: VerdictDrift, wantChange: "stop requiring " + strings.Join([]string{ctxSetup, ctxDepGraph, ctxRelease, ctxGhost}, ", "),
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, []string{ctxGoBuild}, checkContexts(h.repo().ruleset(RulesetName)))
			},
		},
		{
			name: "protection: unknown reports remove nothing, the classic checks carry over", step: StepProtection,
			seed: func(h *harness) {
				h.runner.Checks = nil
				r := h.gh.addRepo(owner, name)
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: false, checks: []string{ctxGoBuild, "execute-smoke-test"}}
			},
			wantCheck: VerdictDrift, wantChange: `create ruleset "devctl: default branch"`,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.NotContains(t, strings.Join(res.Step(StepProtection).Changes, "; "), "stop requiring")
				require.Equal(t, []string{ctxGoBuild, "execute-smoke-test"}, checkContexts(h.repo().ruleset(RulesetName)))
			},
		},
		{
			name: "protection: a declared context is required before it has reported", step: StepProtection, entry: requiredChecksEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: false, checks: []string{ctxGoBuild}}
			},
			wantCheck: VerdictDrift, wantChange: "require " + ctxValidate,
			verify: func(t *testing.T, h *harness, _ *Result) {
				rs := h.repo().ruleset(RulesetName)
				require.Equal(t, []string{ctxValidate, ctxGoBuild}, checkContexts(rs), "declared first, whatever reported")
				require.Equal(t, int64(15368), rs.Rules.RequiredStatusChecks.RequiredStatusChecks[0].GetIntegrationID(), "a declared gate is a GitHub Actions check")
			},
		},
		{
			name: "protection: a declared context that never reported is no ghost", step: StepProtection, entry: requiredChecksEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{actionsCheck(ctxValidate), statusCheck(ctxGoBuild), statusCheck(ctxGhost)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictDrift, wantChange: "stop requiring " + ctxGhost,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{ctxValidate, ctxGoBuild}, checkContexts(h.repo().ruleset(RulesetName)), "the ghost goes, the declared context stays")
				require.Equal(t, []string{"stop requiring " + ctxGhost}, res.Step(StepProtection).Changes)
			},
		},
		{
			name: "protection: strict checks are switched off", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				rs := r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
				rs.Rules.RequiredStatusChecks.StrictRequiredStatusChecksPolicy = true
			},
			wantCheck: VerdictDrift, wantChange: "strict checks true → false",
			verify: func(t *testing.T, h *harness, res *Result) {
				rs := h.repo().ruleset(RulesetName)
				require.False(t, rs.Rules.RequiredStatusChecks.StrictRequiredStatusChecksPolicy)
				require.Equal(t, []string{ctxGoBuild}, checkContexts(rs), "the required checks stay")
				require.Equal(t, []string{"strict checks true → false"}, res.Step(StepProtection).Changes)
			},
		},
		{
			name: "protection: an aligned ruleset plans nothing", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.checkRuns = []string{ctxSemantic}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{actionsCheck(ctxSemantic), statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, `main: ruleset "devctl: default branch"; required: `+ctxSemantic+", "+ctxGoBuild, res.Step(StepProtection).Summary)
			},
		},
		{
			name: "protection: the admins added by hand, in another order, are converged", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, adminBypass(), appBypass(testAppID), teamBypass(testTeamID))
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Len(t, h.repo().ruleset(RulesetName).BypassActors, 3, "the bypass list is compared as a set")
			},
		},
		{
			name: "protection: a read identity gets no bypass actors, so the list is not compared", step: StepProtection,
			seed: func(h *harness) {
				h.gh.readIdentity = true
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepProtection)
				require.Equal(t, `main: ruleset "devctl: default branch"; required: `+ctxGoBuild+"; bypass actors not readable by this identity, not compared", sr.Summary)
				require.True(t, res.Converged)
				require.Equal(t, 0, h.gh.reads("/orgs/"+owner+"/teams/"+team), "the team is not read: nothing to compare it with")
				require.Equal(t, []*github.BypassActor{appBypass(testAppID), adminBypass(), teamBypass(testTeamID)}, h.repo().ruleset(RulesetName).BypassActors, "the ruleset is left as it is")
			},
		},
		{
			name: "protection: a read identity's ruleset without actors is not drift either", step: StepProtection,
			seed: func(h *harness) {
				h.gh.readIdentity = true
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)})
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Contains(t, res.Step(StepProtection).Summary, "bypass actors not readable by this identity, not compared")
				require.Empty(t, h.repo().ruleset(RulesetName).BypassActors, "a list the identity cannot read is not written")
			},
		},
		{
			name: "protection: the rules are repaired as data", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				rs := r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
				rs.Enforcement = github.RulesetEnforcementEvaluate
				rs.Conditions.RefName.Include = []string{"refs/heads/main"}
				rs.Rules.PullRequest.RequiredApprovingReviewCount = 2
				rs.Rules.Deletion = nil
				rs.Rules.RequiredSignatures = &github.EmptyRuleParameters{} // not the engine's rule
			},
			wantCheck: VerdictDrift, wantChange: "enforcement evaluate → active; target refs/heads/main → ~DEFAULT_BRANCH; required reviews 2 → 1; forbid deletions",
			verify: func(t *testing.T, h *harness, _ *Result) {
				rs := h.repo().ruleset(RulesetName)
				require.Equal(t, github.RulesetEnforcementActive, rs.Enforcement)
				require.Equal(t, []string{"~DEFAULT_BRANCH"}, rs.Conditions.RefName.Include)
				require.Equal(t, 1, rs.Rules.PullRequest.RequiredApprovingReviewCount)
				require.NotNil(t, rs.Rules.Deletion)
				require.NotNil(t, rs.Rules.RequiredSignatures, "a rule the engine does not manage stays")
				require.Len(t, h.repo().rulesets, 1, "the ruleset is updated, not recreated")
			},
		},
		{
			name: "protection: the bypass actors are added to a ruleset without one", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)})
			},
			wantCheck: VerdictDrift, wantChange: "bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests",
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []*github.BypassActor{appBypass(testAppID), adminBypass(), teamBypass(testTeamID)}, h.repo().ruleset(RulesetName).BypassActors)
				require.Equal(t, []string{"bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests"}, res.Step(StepProtection).Changes)
			},
		},
		{
			name: "protection: the owning team is added beside the App and nothing else changes", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.checkRuns = []string{ctxSemantic}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{actionsCheck(ctxSemantic), statusCheck(ctxGoBuild)}, appBypass(testAppID))
			},
			wantCheck: VerdictDrift, wantChange: "bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests",
			verify: func(t *testing.T, h *harness, res *Result) {
				rs := h.repo().ruleset(RulesetName)
				require.Equal(t, []*github.BypassActor{appBypass(testAppID), adminBypass(), teamBypass(testTeamID)}, rs.BypassActors, "the admins and the team join the App, in pull_request mode")
				require.Equal(t, []string{ctxSemantic, ctxGoBuild}, checkContexts(rs), "the required checks stay")
				require.Equal(t, 1, rs.Rules.PullRequest.RequiredApprovingReviewCount)
				require.Equal(t, []string{"bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests"}, res.Step(StepProtection).Changes, "one change: the bypass list")
				require.Len(t, h.repo().rulesets, 1, "the ruleset is updated, not recreated")
				require.Equal(t, 2, h.gh.reads("/orgs/"+owner+"/teams/"+team), "the team is read once per run: the check and the repair")
			},
		},
		{
			name: "protection: the repository admins join a ruleset written with the App and the team", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), teamBypass(testTeamID))
			},
			wantCheck: VerdictDrift, wantChange: "bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests",
			verify: func(t *testing.T, h *harness, res *Result) {
				rs := h.repo().ruleset(RulesetName)
				require.Equal(t, []*github.BypassActor{appBypass(testAppID), adminBypass(), teamBypass(testTeamID)}, rs.BypassActors, "the admins join, the rest stays")
				require.Equal(t, []string{ctxGoBuild}, checkContexts(rs))
				require.Len(t, h.repo().rulesets, 1, "the ruleset is updated, not recreated")
			},
		},
		{
			name: "protection: a secret team cannot bypass, so the App and the admins stand and the team is reported", step: StepProtection,
			seed: func(h *harness) {
				h.gh.orgTeams[team].Privacy = new("secret")
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
			},
			wantCheck: VerdictDrift, wantChange: `create ruleset "devctl: default branch"; require ` + ctxGoBuild + "; bypass actors: App 424242 on pull requests, repository admins on pull requests",
			wantFinding: FindingTeamBypassRefused, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []*github.BypassActor{appBypass(testAppID), adminBypass()}, h.repo().ruleset(RulesetName).BypassActors, "the App and the admins")
				sr := res.Step(StepProtection)
				require.Equal(t, []FindingKind{FindingTeamBypassRefused}, kinds(sr.Findings))
				require.Contains(t, sr.Findings[0].Message, "team "+team+" is secret")
				require.Contains(t, sr.Findings[0].Fix, "privacy: closed")
				require.False(t, sr.Findings[0].Advisory, "a person must act before a member's pull request merges through the API")
			},
		},
		{
			name: "protection: agentMerge false plans a ruleset without bypass actors", step: StepProtection, entry: agentMergeFalseEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
			},
			wantCheck: VerdictDrift, wantChange: `create ruleset "devctl: default branch"; require ` + ctxGoBuild,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.NotContains(t, strings.Join(res.Step(StepProtection).Changes, "; "), "bypass")
				require.Empty(t, h.repo().ruleset(RulesetName).BypassActors)
				require.Empty(t, res.Step(StepProtection).Findings, "the opt-out needs no App id")
				require.Zero(t, h.gh.reads("/orgs/"+owner+"/teams/"+team), "the opt-out reads no team")
			},
		},
		{
			name: "protection: agentMerge false removes the bypass actors", step: StepProtection, entry: agentMergeFalseEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictDrift, wantChange: "remove bypass actors",
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.repo().ruleset(RulesetName).BypassActors)
				require.Equal(t, []string{"remove bypass actors"}, res.Step(StepProtection).Changes)
			},
		},
		{
			name: "protection: without the App id classic protection is written as before and the switch is reported", step: StepProtection,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild, ctxSetup}
				r.checkRuns = []string{ctxSemantic}
			},
			wantCheck: VerdictDrift, wantChange: "protect main; require " + ctxSemantic + ", " + ctxGoBuild, wantFinding: FindingRulesetsNotEnabled, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				p := h.repo().protection
				require.NotNil(t, p, "classic protection, as on a run before rulesets")
				require.Equal(t, []string{ctxSemantic, ctxGoBuild}, p.checks)
				require.Equal(t, 1, p.reviews)
				require.True(t, p.enforceAdmins)
				require.False(t, p.strict)
				require.Empty(t, h.repo().rulesets, "a ruleset never appears without the App id")
				f := res.Step(StepProtection).Findings
				require.Len(t, f, 1)
				require.True(t, f[0].Advisory, "the switch is for a person; the repository is protected as declared")
				require.Contains(t, f[0].Fix, "--devctl-app-id")
				require.True(t, res.Converged, "a run without the App id reads converged, as before rulesets")
			},
		},
		{
			name: "protection: without the App id administrators are bound too, as before", step: StepProtection,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: false, strict: true, checks: []string{ctxGoBuild}}
			},
			wantCheck: VerdictDrift, wantChange: "enforce admins false → true; strict checks true → false", wantFinding: FindingRulesetsNotEnabled, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.True(t, h.repo().protection.enforceAdmins)
				require.False(t, h.repo().protection.strict)
				require.Equal(t, []string{"enforce admins false → true; strict checks true → false"}, res.Step(StepProtection).Changes)
				require.Empty(t, h.repo().rulesets)
			},
		},
		{
			name: "protection: without the App id an aligned ruleset reads converged, the bypass list not compared", step: StepProtection,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.checkRuns = []string{ctxSemantic}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{actionsCheck(ctxSemantic), statusCheck(ctxGoBuild)}, appBypass(testAppID), teamBypass(testTeamID))
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepProtection)
				require.Equal(t, `main: ruleset "devctl: default branch"; required: `+ctxSemantic+", "+ctxGoBuild+"; bypass actors not compared (no devctl App id)", sr.Summary)
				require.Empty(t, sr.Findings, "no switch to report: the repository is on the ruleset")
				require.True(t, res.Converged)
				require.Nil(t, h.repo().protection, "no classic protection is written")
				require.Equal(t, 0, h.gh.reads("/orgs/"+owner+"/teams/"+team), "the team is not read: the bypass list is not compared")
			},
		},
		{
			name: "protection: without the App id a ruleset without bypass actors is not drift", step: StepProtection,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)})
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.repo().ruleset(RulesetName).BypassActors, "a run without the App id writes no bypass actor")
				require.True(t, res.Converged)
			},
		},
		{
			name: "protection: without the App id a ruleset that differs is reported for the run with the id, not written", step: StepProtection,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				rs := r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), teamBypass(testTeamID))
				rs.Rules.PullRequest.RequiredApprovingReviewCount = 2
				rs.Rules.RequiredStatusChecks.StrictRequiredStatusChecksPolicy = true
			},
			wantCheck: VerdictReported, wantFinding: FindingRulesetPending, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepProtection)
				require.Equal(t, []FindingKind{FindingRulesetPending}, kinds(sr.Findings))
				require.Equal(t, "the ruleset differs from the declared protection: required reviews 2 → 1; strict checks true → false", sr.Findings[0].Message)
				require.Contains(t, sr.Findings[0].Fix, "--devctl-app-id")
				require.False(t, sr.Findings[0].Advisory, "the repository is not as declared")
				require.Equal(t, 2, h.repo().ruleset(RulesetName).Rules.PullRequest.RequiredApprovingReviewCount, "nothing is written without the App id")
				require.False(t, res.Converged)
			},
		},
		{
			name: "protection: without the App id a ruleset beside classic protection is the switch pending, nothing written", step: StepProtection,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: false, checks: []string{ctxGoBuild}}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID))
			},
			wantCheck: VerdictReported, wantFinding: FindingRulesetPending, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepProtection)
				require.Equal(t, []FindingKind{FindingRulesetPending}, kinds(sr.Findings), "no rulesets-not-enabled: the ruleset exists")
				require.Equal(t, "the switch to the ruleset is pending: classic protection of main still stands beside it", sr.Findings[0].Message)
				require.Equal(t, []*github.BypassActor{appBypass(testAppID)}, h.repo().ruleset(RulesetName).BypassActors, "a run without the App id touches no ruleset")
				require.NotNil(t, h.repo().protection, "and removes no classic protection")
				require.Equal(t, `main: ruleset "devctl: default branch"; required: `+ctxGoBuild+"; bypass actors not compared (no devctl App id)", sr.Summary)
				require.False(t, res.Converged, "the switch is the reconciler's to make")
			},
		},
		{
			name: "protection: an enforcing ruleset the engine did not create is left alone and reported", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset("renovate-automerge", nil, &github.BypassActor{ActorID: new(int64(2740)), ActorType: new(github.BypassActorTypeIntegration), BypassMode: new(github.BypassModeAlways)})
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictReported, wantFinding: FindingForeignRuleset, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				foreign := h.repo().ruleset("renovate-automerge")
				require.NotNil(t, foreign)
				require.Equal(t, github.BypassModeAlways, *foreign.BypassActors[0].BypassMode, "untouched")
				f := res.Step(StepProtection).Findings[0]
				require.True(t, f.Advisory, "a foreign ruleset does not keep the repository from converging")
				require.Contains(t, f.Message, `"renovate-automerge"`)
				require.Contains(t, f.Fix, "pruneRulesets", "the fix names the opt-in")
				require.True(t, res.Converged)
			},
		},
		{
			name: "protection: under pruneRulesets an undeclared active ruleset is deleted, declared, disabled and evaluate ones are not", step: StepProtection, entry: pruneRulesetsEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset("renovate-automerge", nil)
				r.addRuleset("protect-giantswarm", nil, adminBypass())
				r.addRuleset("Code Quality Copilot review for default branch", nil).Enforcement = github.RulesetEnforcementDisabled
				r.addRuleset("trial", nil).Enforcement = github.RulesetEnforcementEvaluate
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictDrift, wantChange: `delete ruleset "renovate-automerge" (not declared, pruneRulesets)`, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Nil(t, h.repo().ruleset("renovate-automerge"), "deleted")
				require.NotNil(t, h.repo().ruleset("protect-giantswarm"), "declared: kept")
				require.NotNil(t, h.repo().ruleset("Code Quality Copilot review for default branch"), "disabled: kept")
				require.NotNil(t, h.repo().ruleset("trial"), "evaluate: kept")
				require.NotNil(t, h.repo().ruleset(RulesetName))
				sr := res.Step(StepProtection)
				require.Equal(t, []string{`delete ruleset "renovate-automerge" (not declared, pruneRulesets)`}, sr.Changes)
				require.Equal(t, []FindingKind{FindingForeignRuleset}, kinds(sr.Findings), "the evaluate ruleset stays the advisory")
				require.Contains(t, sr.Findings[0].Message, `"trial"`)
			},
		},
		{
			name: "protection: lineGate adds the sync's App to the default branch's ruleset and writes the gate no actor bypasses", step: StepProtection, entry: lineGateEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictDrift, wantChange: `create ruleset "devctl: line gate" on ~DEFAULT_BRANCH, refs/heads/release-1.3, deletion forbidden, no bypass actor; require run-tests, govulncheck`,
			verify: func(t *testing.T, h *harness, res *Result) {
				line := &github.BypassActor{ActorID: new(int64(414149)), ActorType: new(github.BypassActorTypeIntegration), BypassMode: new(github.BypassModeAlways)}
				require.Equal(t, []*github.BypassActor{appBypass(testAppID), adminBypass(), teamBypass(testTeamID), line}, h.repo().ruleset(RulesetName).BypassActors, "the sync's App passes the pull-request rule in every mode")
				gate := h.repo().ruleset(LineGateRulesetName)
				require.NotNil(t, gate)
				require.Empty(t, gate.BypassActors, "no actor bypasses the gate")
				require.Equal(t, []string{"~DEFAULT_BRANCH", "refs/heads/release-1.3"}, gate.Conditions.RefName.Include)
				require.Equal(t, []*github.RuleStatusCheck{actionsCheck("run-tests"), actionsCheck("govulncheck")}, gate.Rules.RequiredStatusChecks.RequiredStatusChecks, "pinned to GitHub Actions")
				require.NotNil(t, gate.Rules.Deletion)
				require.Nil(t, gate.Rules.PullRequest, "a checked commit lands by push")
				require.Nil(t, gate.Rules.NonFastForward, "the sync's land replaces the line")
				require.Equal(t, []string{
					"bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests, App 414149 always",
					`create ruleset "devctl: line gate" on ~DEFAULT_BRANCH, refs/heads/release-1.3, deletion forbidden, no bypass actor; require run-tests, govulncheck`,
				}, res.Step(StepProtection).Changes)
			},
		},
		{
			name: "protection: lineGate alongside agentMerge false keeps the sync's App alone", step: StepProtection, entry: lineGateEntryYAML + "  agentMerge: false\n",
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)})
			},
			wantCheck: VerdictDrift, wantChange: "bypass actor: App 414149 always",
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Len(t, h.repo().ruleset(RulesetName).BypassActors, 1)
				require.NotNil(t, h.repo().ruleset(LineGateRulesetName))
			},
		},
		{
			name: "protection: the align-files App id moves the review into a ruleset the App bypasses, the checks stay binding", step: StepProtection,
			seed: func(h *harness) {
				h.runner.AlignFilesAppID = testAlignAppID
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictDrift, wantChange: `create ruleset "devctl: review", 1 required review(s)`,
			verify: func(t *testing.T, h *harness, res *Result) {
				review := h.repo().ruleset(ReviewRulesetName)
				require.NotNil(t, review)
				require.Equal(t, []*github.BypassActor{appBypass(testAppID), adminBypass(), teamBypass(testTeamID), appBypass(testAlignAppID), appBypass(renovateAppID)}, review.BypassActors, "the align-files and Renovate Apps bypass the review beside the default branch's actors")
				require.Equal(t, 1, review.Rules.PullRequest.RequiredApprovingReviewCount)
				require.Nil(t, review.Rules.RequiredStatusChecks, "the review ruleset carries no checks")
				main := h.repo().ruleset(RulesetName)
				require.Nil(t, main.Rules.PullRequest, "the review rule moved out")
				require.Equal(t, []string{ctxGoBuild}, checkContexts(main), "the checks stay where the align-files App has no bypass")
				require.NotContains(t, actorKeys(main.BypassActors), actorKey(appBypass(testAlignAppID)))
				require.NotContains(t, actorKeys(main.BypassActors), actorKey(appBypass(renovateAppID)), "the Renovate App waits for the checks")
				require.Equal(t, []string{
					`create ruleset "devctl: review", 1 required review(s); bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests, App 434343 on pull requests, App 2740 on pull requests`,
					"stop requiring pull requests",
				}, res.Step(StepProtection).Changes, "the review ruleset is written before the default branch drops the rule")
			},
		},
		{
			name: "protection: a review ruleset without the Renovate App gains it, the checks stay binding", step: StepProtection,
			seed: func(h *harness) {
				h.runner.AlignFilesAppID = testAlignAppID
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID)).Rules.PullRequest = nil
				review := r.addRuleset(ReviewRulesetName, nil, appBypass(testAppID), adminBypass(), teamBypass(testTeamID), appBypass(testAlignAppID))
				review.Rules.Deletion, review.Rules.NonFastForward = nil, nil
			},
			wantCheck: VerdictDrift, wantChange: "bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests, App 434343 on pull requests, App 2740 on pull requests",
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{"bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests, App 434343 on pull requests, App 2740 on pull requests"}, res.Step(StepProtection).Changes, "one change: the review ruleset's bypass list")
				require.Contains(t, actorKeys(h.repo().ruleset(ReviewRulesetName).BypassActors), actorKey(appBypass(renovateAppID)))
				main := h.repo().ruleset(RulesetName)
				require.NotContains(t, actorKeys(main.BypassActors), actorKey(appBypass(renovateAppID)), "the Renovate App waits for the checks")
				require.Equal(t, []string{ctxGoBuild}, checkContexts(main))
			},
		},
		{
			name: "protection: the release App id adds the release App to a review ruleset without it, the checks stay binding", step: StepProtection,
			seed: func(h *harness) {
				h.runner.AlignFilesAppID = testAlignAppID
				h.runner.ReleaseAppID = testReleaseAppID
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID)).Rules.PullRequest = nil
				review := r.addRuleset(ReviewRulesetName, nil, appBypass(testAppID), adminBypass(), teamBypass(testTeamID), appBypass(testAlignAppID), appBypass(renovateAppID))
				review.Rules.Deletion, review.Rules.NonFastForward = nil, nil
			},
			wantCheck: VerdictDrift, wantChange: "bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests, App 434343 on pull requests, App 2740 on pull requests, App 454545 on pull requests",
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{"bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests, App 434343 on pull requests, App 2740 on pull requests, App 454545 on pull requests"}, res.Step(StepProtection).Changes, "one change: the review ruleset's bypass list")
				require.Contains(t, actorKeys(h.repo().ruleset(ReviewRulesetName).BypassActors), actorKey(appBypass(testReleaseAppID)))
				main := h.repo().ruleset(RulesetName)
				require.NotContains(t, actorKeys(main.BypassActors), actorKey(appBypass(testReleaseAppID)), "the release App waits for the checks")
				require.Equal(t, []string{ctxGoBuild}, checkContexts(main))
			},
		},
		{
			name: "protection: a review ruleset with the release App is clean on a run with its id", step: StepProtection,
			seed: func(h *harness) {
				h.runner.AlignFilesAppID = testAlignAppID
				h.runner.ReleaseAppID = testReleaseAppID
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID)).Rules.PullRequest = nil
				review := r.addRuleset(ReviewRulesetName, nil, appBypass(testAppID), adminBypass(), teamBypass(testTeamID), appBypass(testAlignAppID), appBypass(renovateAppID), appBypass(testReleaseAppID))
				review.Rules.Deletion, review.Rules.NonFastForward = nil, nil
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, res.Step(StepProtection).Changes, "the next night repairs nothing")
			},
		},
		{
			name: "protection: the align-files App id leaves agentMerge false's review in the default branch's ruleset, without bypass", step: StepProtection, entry: entryYAML + "  agentMerge: false\n",
			seed: func(h *harness) {
				h.runner.AlignFilesAppID = testAlignAppID
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)})
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Nil(t, h.repo().ruleset(ReviewRulesetName))
				require.NotNil(t, h.repo().ruleset(RulesetName).Rules.PullRequest)
				require.Empty(t, h.repo().ruleset(RulesetName).BypassActors, "nothing bypasses the review, the Renovate App neither")
			},
		},
		{
			name: "protection: without the align-files App id the review returns to the default branch's ruleset before the review ruleset goes", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID)).Rules.PullRequest = nil
				review := r.addRuleset(ReviewRulesetName, nil, appBypass(testAppID), adminBypass(), teamBypass(testTeamID), appBypass(testAlignAppID))
				review.Rules.Deletion, review.Rules.NonFastForward = nil, nil
			},
			wantCheck: VerdictDrift, wantChange: `delete ruleset "devctl: review" (the review rule is back in "devctl: default branch")`,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Nil(t, h.repo().ruleset(ReviewRulesetName))
				require.Equal(t, 1, h.repo().ruleset(RulesetName).Rules.PullRequest.RequiredApprovingReviewCount)
				require.Equal(t, []string{
					"require pull requests; required reviews 0 → 1",
					`delete ruleset "devctl: review" (the review rule is back in "devctl: default branch")`,
				}, res.Step(StepProtection).Changes)
				require.Empty(t, res.Step(StepProtection).Findings, "the engine's own ruleset is never foreign")
			},
		},
		{
			name: "protection: without the devctl App id a missing review ruleset is reported for the run with the id, not written", step: StepProtection,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				h.runner.AlignFilesAppID = testAlignAppID
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)})
			},
			wantCheck: VerdictReported, wantFinding: FindingRulesetPending, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Nil(t, h.repo().ruleset(ReviewRulesetName))
				require.NotNil(t, h.repo().ruleset(RulesetName).Rules.PullRequest, "nothing written")
				var messages []string
				for _, f := range res.Step(StepProtection).Findings {
					messages = append(messages, f.Message)
				}
				require.Contains(t, strings.Join(messages, "\n"), `the ruleset "devctl: review" differs from the declared review rule: create ruleset`)
			},
		},
		{
			name: "protection: a run without either App id reads the review ruleset it finds as the layout, not a drift", step: StepProtection,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID)).Rules.PullRequest = nil
				review := r.addRuleset(ReviewRulesetName, nil, appBypass(testAppID), adminBypass(), teamBypass(testTeamID), appBypass(testAlignAppID))
				review.Rules.Deletion, review.Rules.NonFastForward = nil, nil
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.NotNil(t, h.repo().ruleset(ReviewRulesetName))
				require.Empty(t, res.Step(StepProtection).Findings)
			},
		},
		{
			name: "protection: without lineGate the gate ruleset is deleted", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
				r.addRuleset(LineGateRulesetName, []*github.RuleStatusCheck{actionsCheck("run-tests")})
			},
			wantCheck: VerdictDrift, wantChange: `delete ruleset "devctl: line gate" (no lineGate)`,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Nil(t, h.repo().ruleset(LineGateRulesetName))
				require.Empty(t, res.Step(StepProtection).Findings, "the engine's own ruleset is never foreign")
			},
		},
		{
			name: "protection: without the App id a missing gate is reported for the run with the id, not written", step: StepProtection, entry: lineGateEntryYAML,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)})
			},
			wantCheck: VerdictReported, wantFinding: FindingRulesetPending, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Nil(t, h.repo().ruleset(LineGateRulesetName))
				require.Contains(t, res.Step(StepProtection).Findings[0].Message, `the ruleset "devctl: line gate" differs from the declared lineGate: create ruleset`)
			},
		},
		{
			name: "protection: under pruneRulesets a run without the App id reports the undeclared ruleset for the run with the id, deletes nothing", step: StepProtection, entry: pruneRulesetsEntryYAML,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset("renovate-automerge", nil)
				r.addRuleset("protect-giantswarm", nil, adminBypass())
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)})
			},
			wantCheck: VerdictReported, wantFinding: FindingRulesetPending, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.NotNil(t, h.repo().ruleset("renovate-automerge"), "a run without the App id deletes no ruleset")
				sr := res.Step(StepProtection)
				require.Equal(t, []FindingKind{FindingRulesetPending}, kinds(sr.Findings))
				require.Contains(t, sr.Findings[0].Message, `"renovate-automerge"`)
				require.False(t, res.Converged, "the deletion is the reconciler's to make")
			},
		},
		{
			name: "protection: a disabled ruleset the engine did not create is not reported", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				copilot := r.addRuleset("Code Quality Copilot review for default branch", nil)
				copilot.Enforcement = github.RulesetEnforcementDisabled
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, res.Step(StepProtection).Findings, "a ruleset enforcing nothing leaves a person nothing to weigh")
				require.NotNil(t, h.repo().ruleset("Code Quality Copilot review for default branch"), "untouched")
				require.True(t, res.Converged)
			},
		},
		{
			name: "protection: a ruleset the entry declares is kept without a finding", step: StepProtection, entry: declaredRulesetEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset("protect-giantswarm", nil, adminBypass())
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, res.Step(StepProtection).Findings, "the entry records the decision to keep it")
				kept := h.repo().ruleset("protect-giantswarm")
				require.NotNil(t, kept)
				require.Equal(t, []*github.BypassActor{adminBypass()}, kept.BypassActors, "untouched")
				require.True(t, res.Converged)
			},
		},
		{
			name: "protection: a declared ruleset the repository does not carry is reported", step: StepProtection, entry: declaredRulesetEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictReported, wantFinding: FindingDeclaredRulesetMissing, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Len(t, h.repo().rulesets, 1, "the engine creates no ruleset but its own")
				f := res.Step(StepProtection).Findings[0]
				require.True(t, f.Advisory, "a declaration without its ruleset does not keep the repository from converging")
				require.Contains(t, f.Message, `"protect-giantswarm"`)
				require.True(t, res.Converged)
			},
		},
		{
			name: "protection: a classic protection beside an aligned ruleset is removed alone", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: false, checks: []string{ctxGoBuild}}
			},
			wantCheck: VerdictDrift, wantChange: "remove classic protection of main",
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{"remove classic protection of main"}, res.Step(StepProtection).Changes)
				require.Nil(t, h.repo().protection)
				require.Len(t, h.repo().rulesets, 1)
			},
		},
		{
			// The reconciler's person-run twin: a project with a GitHub App
			// definition and an enabled trigger is the pipeline. The implicit
			// OAuth definition beside it has no trigger and is no finding.
			name: "circleci: a GitHub App pipeline with an enabled trigger reads ok", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.cc.addApp(owner, name)
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "GitHub App pipeline build", res.Step(StepCircleCI).Summary)
				require.Empty(t, res.Findings())
				require.Empty(t, h.cc.mutations, "the engine writes nothing on CircleCI")
			},
		},
		{
			// An OAuth definition that is triggered beside the App's builds
			// every push twice: the advisory circleci-oauth-follow names the
			// migration, and the run converges.
			name: "circleci: an App pipeline with a triggered OAuth definition beside it is an advisory double build", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				p := h.cc.addApp(owner, name)
				p.definitions[0].triggers = []circleciclient.Trigger{{EventSource: circleciclient.DefinitionSource{Provider: circleciclient.ProviderGitHubOAuth}}}
			},
			wantCheck: VerdictReported, wantFinding: FindingCircleCIOAuthFollow, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepCircleCI)
				require.Contains(t, sr.Summary, "GitHub App pipeline build; OAuth pipeline "+name+" triggered too")
				require.Len(t, sr.Findings, 1)
				require.True(t, sr.Findings[0].Advisory)
				require.Contains(t, sr.Findings[0].Message, "every push builds twice")
				require.Contains(t, sr.Findings[0].Fix, "devctl repo follow-circleci "+owner+"/"+name+" --migrate")
				require.True(t, sr.Converges(), "an advisory finding keeps the run converging")
				require.Empty(t, h.cc.mutations, "the OAuth trigger is deleted by a person, not by the engine")
			},
		},
		{
			// Only the OAuth definition is triggered: it builds through the
			// GitHub grant of whoever followed the project and stops when
			// they leave. The fix migrates, deleting the OAuth trigger.
			name: "circleci: an OAuth-only project is an advisory OAuth follow with --migrate in the fix", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.cc.addOAuth(owner, name)
			},
			wantCheck: VerdictReported, wantFinding: FindingCircleCIOAuthFollow, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepCircleCI)
				require.Equal(t, "OAuth follow, no GitHub App pipeline", sr.Summary)
				require.Len(t, sr.Findings, 1)
				require.True(t, sr.Findings[0].Advisory)
				require.Contains(t, sr.Findings[0].Message, "OAuth pipeline "+name)
				require.Contains(t, sr.Findings[0].Fix, "devctl repo follow-circleci "+owner+"/"+name+" --migrate")
				require.True(t, sr.Converges(), "it builds today: the fleet does not flip to non-converged")
				require.Empty(t, h.cc.mutations, "nothing is followed, created or migrated by the engine")
			},
		},
		{
			// A GitHub App definition whose only trigger is disabled builds
			// nothing: the pipeline is missing, and with no OAuth trigger the
			// fix needs no --migrate.
			name: "circleci: a GitHub App definition whose only trigger is disabled is no pipeline", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				p := h.cc.addApp(owner, name)
				p.definitions[1].triggers[0].Disabled = true
			},
			wantCheck: VerdictReported, wantFinding: FindingCircleCIPipelineMissing, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				f := res.Step(StepCircleCI).Findings[0]
				require.Contains(t, f.Fix, "devctl repo follow-circleci "+owner+"/"+name)
				require.NotContains(t, f.Fix, "--migrate")
				require.Empty(t, h.cc.mutations)
			},
		},
		{
			// A GitHub App definition without any trigger starts no pipeline.
			name: "circleci: a GitHub App definition without a trigger is no pipeline", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.cc.project(owner, name).addDefinition("build", circleciclient.ProviderGitHubApp, nil)
			},
			wantCheck: VerdictReported, wantFinding: FindingCircleCIPipelineMissing, wantAfter: VerdictReported,
		},
		{
			// CircleCI knows no project: nothing builds the repository. The
			// step reports and never follows (the fake would record the POST).
			name: "circleci: no CircleCI project is reported and never followed", step: StepCircleCI,
			seed:      func(h *harness) { h.gh.addRepo(owner, name) },
			wantCheck: VerdictReported, wantFinding: FindingCircleCIPipelineMissing, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepCircleCI)
				require.Equal(t, "no CircleCI project", sr.Summary)
				require.Contains(t, sr.Findings[0].Fix, "devctl repo follow-circleci "+owner+"/"+name)
				require.NotContains(t, sr.Findings[0].Fix, "--migrate")
				require.Empty(t, h.cc.projects, "no project is created")
				require.Empty(t, h.cc.mutations)
				require.Empty(t, h.mutations(), "no grant, no deploy key, no setting")
			},
		},
		{
			// A renamed repository: CircleCI keeps the project under the new
			// slug, which is the one the step reads.
			name: "circleci: a renamed repository is read under its new slug", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name+"-v2")
				h.gh.redirects[owner+"/"+name] = owner + "/" + name + "-v2"
				h.cc.addApp(owner, name+"-v2")
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "GitHub App pipeline build", res.Step(StepCircleCI).Summary)
			},
		},
		{
			// The reconciler holds no CircleCI client: the statuses of the
			// default branch's head say who builds. The App's login is a
			// GitHub App pipeline.
			name: "circleci: without a client, App statuses on the default branch read ok", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.addStatus("main", "go-build", "success", "wf-1", CircleCIAppLogin, time.Now().Add(-time.Hour))
				h.runner.CircleCI = nil
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "CircleCI statuses on main from "+CircleCIAppLogin+": GitHub App pipeline", res.Step(StepCircleCI).Summary)
				require.Empty(t, res.Findings())
				require.Zero(t, h.runner.CircleCIRequests.Count(), "no CircleCI request")
			},
		},
		{
			// Statuses posted by a person are an OAuth follow's: it stops
			// building when that person leaves. The fix migrates.
			name: "circleci: without a client, statuses posted by a person are an OAuth follow", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.addStatus("main", "go-build", "success", "wf-1", "alice", time.Now().Add(-time.Hour))
				h.runner.CircleCI = nil
			},
			wantCheck: VerdictReported, wantFinding: FindingCircleCIOAuthFollow, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepCircleCI)
				require.Equal(t, "OAuth follow, no GitHub App pipeline", sr.Summary)
				require.Contains(t, sr.Findings[0].Message, "alice")
				require.Contains(t, sr.Findings[0].Fix, "devctl repo follow-circleci "+owner+"/"+name+" --migrate")
				require.True(t, sr.Converges(), "advisory")
				require.Empty(t, h.mutations())
			},
		},
		{
			// The App and a person both post on the head: two pipelines build
			// every push. The advisory finding names the migration.
			name: "circleci: without a client, statuses from the App and a person are an advisory double build", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.addStatus("main", "go-build", "success", "wf-1", "alice", time.Now().Add(-time.Hour))
				r.addStatus("main", "go-build", "success", "wf-2", CircleCIAppLogin, time.Now().Add(-time.Hour))
				h.runner.CircleCI = nil
			},
			wantCheck: VerdictReported, wantFinding: FindingCircleCIOAuthFollow, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepCircleCI)
				require.Contains(t, sr.Findings[0].Message, "every push builds twice")
				require.Contains(t, sr.Findings[0].Fix, "--migrate")
				require.True(t, sr.Converges())
			},
		},
		{
			// The head was pushed seconds ago (or skips CI) and carries no
			// status yet; the commit before it does, from the App: ok.
			name: "circleci: without a client, the App's statuses on a recent commit before the head read ok", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.history = []*github.RepositoryCommit{{SHA: new("older")}}
				r.addStatus("older", "go-build", "success", "wf-1", CircleCIAppLogin, time.Now().Add(-time.Hour))
				h.runner.CircleCI = nil
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Contains(t, res.Step(StepCircleCI).Summary, "GitHub App pipeline")
			},
		},
		{
			// Neither the App nor a person posted a CircleCI status on the
			// head; another system's status is no CircleCI status.
			name: "circleci: without a client, no CircleCI status on the head is a missing pipeline", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.refStatuses = map[string][]fakeStatus{"main": {{context: "semantic-pull-request", state: "success", creator: "alice", at: time.Now()}}}
				h.runner.CircleCI = nil
			},
			wantCheck: VerdictReported, wantFinding: FindingCircleCIPipelineMissing, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				sr := res.Step(StepCircleCI)
				require.Equal(t, "no CircleCI status on main", sr.Summary)
				require.Contains(t, sr.Findings[0].Fix, "devctl repo follow-circleci "+owner+"/"+name)
				require.NotContains(t, sr.Findings[0].Fix, "--migrate")
				require.Empty(t, h.mutations())
			},
		},
		{
			// A configuration repository, or one released by GitHub Actions,
			// has no .circleci/config.yml and declares no generated pipeline:
			// CircleCI has nothing to build, so neither step reads CircleCI
			// or reports a missing pipeline or tag build.
			name: "circleci: a repository without a pipeline is not read, its release not verified", step: StepCircleCI,
			entry: configurationEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				delete(r.files, ".circleci/config.yml")
				delete(r.files, ".circleci/workflows.yml")
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) {
				config := "/repos/" + owner + "/" + name + "/contents/" + circleCIConfig
				before := h.gh.reads(config)
				res := h.run(ModeCheck, false, StepCircleCI, StepRelease)
				for _, step := range []Step{StepCircleCI, StepRelease} {
					require.Equal(t, VerdictSkipped, res.Step(step).Verdict, "%s: %+v", step, res.Step(step))
					require.Equal(t, "no CircleCI pipeline", res.Step(step).Summary)
					require.Empty(t, res.Step(step).Findings)
				}
				require.Equal(t, before+1, h.gh.reads(config), "one read of the branch serves both steps")
				require.Empty(t, h.mutations())
			},
		},
		{
			// A template repository's .circleci/config.yml is content for the
			// repositories created from it (gen.ci.templateContent): the
			// branch carries the file, yet CircleCI has nothing to build, so
			// no pipeline is looked for, its release is not held against a
			// build, and the branch is not read.
			name: "circleci: template content is no pipeline, whatever the branch carries", step: StepCircleCI, existing: true,
			entry: templateContentEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) {
				res := h.run(ModeCheck, false, StepCircleCI, StepRelease)
				for _, step := range []Step{StepCircleCI, StepRelease} {
					require.Equal(t, VerdictSkipped, res.Step(step).Verdict, "%s: %+v", step, res.Step(step))
					require.Equal(t, "no CircleCI pipeline: .circleci/config.yml is template content (gen.ci.templateContent)", res.Step(step).Summary)
				}
				require.Zero(t, h.gh.reads("/repos/"+owner+"/"+name+"/contents/"+circleCIConfig), "the declaration decides, the branch is not read")
				require.Empty(t, res.Step(StepRelease).Findings, "a release CircleCI never built is no missed tag build")
			},
		},
		{
			// On a first creation the generated pipeline is not on the branch
			// yet: the entry declares it (gen.ci.generate: true), so the step
			// checks for it without reading the branch. Nothing is created:
			// the pipeline is a person's `devctl repo follow-circleci`.
			name: "circleci: a generated pipeline is looked for before its config is on the branch", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				delete(r.files, ".circleci/config.yml")
				delete(r.files, ".circleci/workflows.yml")
			},
			wantCheck: VerdictReported, wantFinding: FindingCircleCIPipelineMissing, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "no CircleCI project", res.Step(StepCircleCI).Summary)
				require.Zero(t, h.gh.reads("/repos/"+owner+"/"+name+"/contents/"+circleCIConfig), "a declared pipeline is not looked up")
				require.Empty(t, h.cc.mutations)
			},
		},
		{
			// Generated CI without a CircleCI job is GitHub Actions only:
			// gen.ci.generate: true declares no pipeline the CircleCI
			// generator writes, so the branch decides, and a branch without
			// .circleci/config.yml has none. Neither step reads CircleCI, and
			// a project that exists is left as it is.
			name: "circleci: generated CI without a CircleCI job is no pipeline", step: StepCircleCI, existing: true,
			entry: actionsOnlyEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				delete(r.files, ".circleci/config.yml")
				delete(r.files, ".circleci/workflows.yml")
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) {
				res := h.run(ModeCheck, false, StepCircleCI, StepRelease)
				for _, step := range []Step{StepCircleCI, StepRelease} {
					require.Equal(t, VerdictSkipped, res.Step(step).Verdict, "%s: %+v", step, res.Step(step))
					require.Equal(t, "no CircleCI pipeline", res.Step(step).Summary)
					require.Empty(t, res.Step(step).Changes, "%s plans nothing", step)
				}
				require.Empty(t, res.Step(StepRelease).Findings, "a release CircleCI never built is no missed tag build")

				h.cc.addOAuth(owner, name) // a project followed earlier
				res = h.run(ModeRepair, false, StepCircleCI, StepRelease)
				require.Equal(t, VerdictSkipped, res.Step(StepCircleCI).Verdict, "%+v", res.Step(StepCircleCI))
				require.Empty(t, h.mutations(), "a project of a repository without a pipeline is left as it is")
			},
		},
		{
			// The reconciler validates a declared repository's entry as an
			// existing one, rendered as declared: an entry with gen and no
			// gen.ci does not get the creation default gen.ci.generate: true,
			// so the branch decides. A repository released by GitHub Actions
			// has no .circleci/config.yml there: no pipeline is looked for,
			// and its release is not held against a tag build CircleCI never
			// ran.
			name: "circleci: an existing entry without gen.ci and without a config on the branch has no pipeline", step: StepCircleCI, existing: true,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				delete(r.files, ".circleci/config.yml")
				delete(r.files, ".circleci/workflows.yml")
				r.release, r.releaseAt = "v0.6.0", time.Now().Add(-time.Hour)
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.NotContains(t, h.entry.Rendered, "generate:", "an existing entry is rendered as declared")
				res := h.run(ModeCheck, false, StepCircleCI, StepRelease)
				for _, step := range []Step{StepCircleCI, StepRelease} {
					require.Equal(t, VerdictSkipped, res.Step(step).Verdict, "%s: %+v", step, res.Step(step))
					require.Equal(t, "no CircleCI pipeline", res.Step(step).Summary)
				}
				require.Empty(t, res.Step(StepRelease).Findings, "a release CircleCI never built is no missed tag build")
			},
		},
		{
			// The same entry over a hand-maintained .circleci/config.yml: the
			// branch says there is a pipeline, and the step looks for the
			// project's App pipeline.
			name: "circleci: an existing entry without gen.ci checks the pipeline the branch carries", step: StepCircleCI, existing: true,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.cc.addApp(owner, name)
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, 2, h.gh.reads("/repos/"+owner+"/"+name+"/contents/"+circleCIConfig), "the branch is read once per run: the check and the repair")
			},
		},
		{
			name: "webhooks: the baseline's webhook is created", step: StepWebhooks,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.baseline.Webhooks = []Webhook{tekton}
			},
			wantCheck: VerdictDrift, wantChange: "create webhook " + tekton.URL,
			verify: func(t *testing.T, h *harness, _ *Result) {
				hooks := h.repo().hooks
				require.Len(t, hooks, 1)
				require.Equal(t, tekton.Events, hooks[0].Events)
				require.Equal(t, "json", hooks[0].GetConfig().GetContentType())
			},
		},
		{
			name: "renovate: the configuration and the Dependency Dashboard issue", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).addIssue(renovateLogin, "Dependency Dashboard", false)
				h.gh.installation.status = 403 // a GitHub App token cannot list a user's installations
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "renovate.json5; Dependency Dashboard issue #1", res.Step(StepRenovate).Summary)
			},
		},
		{
			name: "renovate: a pull request of Renovate's stands in for the dashboard", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).addIssue(renovateLogin, "fix(deps): update module example.com/dep to v2", true)
				h.gh.installation.status = 403
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "renovate.json5; Renovate pull request #1", res.Step(StepRenovate).Summary)
			},
		},
		{
			name: "renovate: a commit of Renovate's stands in for the dashboard", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).history = []*github.RepositoryCommit{{SHA: new("abc1234def"), Author: &github.User{Login: new(renovateLogin)}}}
				h.gh.installation.status = 403
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "renovate.json5; Renovate commit abc1234 on main", res.Step(StepRenovate).Summary)
			},
		},
		{
			name: "renovate: a configuration without a trace of a run is reported", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.gh.installation.status = 403
			},
			wantCheck: VerdictReported, wantFinding: FindingRenovateNotScanned, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				f := res.Findings()[0]
				require.Contains(t, f.Message, "no Dependency Dashboard issue")
				require.Contains(t, f.Fix, "covers all repositories")
			},
		},
		{
			name: "renovate: no trace on a repository younger than a day is ok, Renovate's first run is not due", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).createdAt = time.Now().Add(-time.Hour)
				h.gh.installation.status = 403
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "renovate.json5; no Renovate run yet and none due: the repository is younger than a day, Renovate's first run follows",
					res.Step(StepRenovate).Summary)
			},
		},
		{
			name: "renovate: no trace on a repository a day old is reported", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).createdAt = time.Now().Add(-25 * time.Hour)
				h.gh.installation.status = 403
			},
			wantCheck: VerdictReported, wantFinding: FindingRenovateNotScanned, wantAfter: VerdictReported,
		},
		{
			name: "renovate: no configuration is reported", step: StepRenovate,
			seed: func(h *harness) {
				delete(h.gh.addRepo(owner, name).files, "renovate.json5")
				h.gh.installation.status = 403
			},
			wantCheck: VerdictReported, wantFinding: FindingRenovateNotScanned, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Contains(t, res.Findings()[0].Fix, "renovate.json5")
			},
		},
		{
			name: "renovate: a configuration that disables Renovate", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files["renovate.json5"] = "{\n  enabled: false,\n}\n"
				h.gh.installation.status = 403
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "renovate.json5 disables Renovate", res.Step(StepRenovate).Summary)
			},
		},
		{
			name: "renovate: false without a configuration is ok, and no trace is read", step: StepRenovate, entry: renovateFalseEntryYAML,
			seed: func(h *harness) {
				delete(h.gh.addRepo(owner, name).files, "renovate.json5")
				h.gh.installation.status = 403
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "renovate: false; no Renovate configuration", res.Step(StepRenovate).Summary)
				require.Empty(t, res.Findings())
			},
		},
		{
			name: "renovate: false with a configuration that disables Renovate is ok", step: StepRenovate, entry: renovateFalseEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files["renovate.json5"] = "{\n  enabled: false,\n}\n"
				h.gh.installation.status = 403
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "renovate: false; renovate.json5 disables Renovate", res.Step(StepRenovate).Summary)
			},
		},
		{
			name: "renovate: false with a configuration that keeps Renovate on is reported", step: StepRenovate, entry: renovateFalseEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).addIssue(renovateLogin, "Dependency Dashboard", false)
				h.gh.installation.status = 403
			},
			wantCheck: VerdictReported, wantFinding: FindingRenovateDeclaredOff, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				f := res.Findings()[0]
				require.Contains(t, f.Message, "declares renovate: false, but renovate.json5 keeps Renovate on")
				require.Contains(t, f.Fix, "delete renovate.json5")
			},
		},
		{
			name: "renovate: the installation's list is detail when the token reads it, never the verdict", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.gh.installation.selection = "all"
			},
			wantCheck: VerdictReported, wantFinding: FindingRenovateNotScanned, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "the installation covers all repositories", res.Step(StepRenovate).Summary, "detail next to the finding")
			},
		},
		{
			name: "renovate: issues out of the token's reach, a commit of Renovate's still proves the run", step: StepRenovate,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.issuesStatus = 403 // a GitHub App without issues: read on a private repository
				r.history = []*github.RepositoryCommit{{SHA: new("abc1234def"), Commit: &github.Commit{Author: &github.CommitAuthor{Name: new("renovate[bot]"), Email: new("29139614+renovate[bot]@users.noreply.github.com")}}}}
				h.gh.installation.status = 403
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "renovate.json5; Renovate commit abc1234 on main", res.Step(StepRenovate).Summary)
			},
		},
		{
			name: "renovate: issues out of the token's reach and no commit is unchecked, naming the permission", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).issuesStatus = 403
				h.gh.installation.status = 403
			},
			wantCheck: VerdictReported, wantFinding: FindingUnchecked, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				f := res.Findings()[0]
				require.Equal(t, FindingUnchecked, f.Kind)
				require.Contains(t, f.Fix, "issues: read")
			},
		},
		{
			name: "codeowners: drift opens one pull request", step: StepCodeowners,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).files["CODEOWNERS"] = "* @giantswarm/team-other\n" },
			wantCheck: VerdictDrift, wantChange: "open a pull request setting CODEOWNERS to @giantswarm/team-bumblebee",
			wantAfter: VerdictDrift, // the pull request awaits its merge; nothing else happens
			verify: func(t *testing.T, h *harness, res *Result) {
				r := h.repo()
				require.Len(t, r.prs, 1)
				require.Equal(t, reposetup.Codeowners(team), r.branchFiles[codeownersBranch]["CODEOWNERS"])
				require.Equal(t, "chore: set CODEOWNERS to @giantswarm/team-bumblebee", r.headSubject(codeownersBranch), "a conventional commit for auto-release")
				require.Equal(t, "* @giantswarm/team-other\n", r.files["CODEOWNERS"], "main is untouched")
				require.Equal(t, []FindingKind{FindingPendingPullRequest}, kinds(res.Step(StepCodeowners).Findings))
			},
		},
		{
			name: "codeowners: without an override the file naming the team is the desired one", step: StepCodeowners,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).files["CODEOWNERS"] = reposetup.Codeowners(team) },
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "names @giantswarm/team-bumblebee", res.Step(StepCodeowners).Summary)
			},
		},
		{
			name: "codeowners: a file equal to the override is ok", step: StepCodeowners,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files["CODEOWNERS"] = testCodeownersOverride
				h.codeownersOverride = []byte(testCodeownersOverride)
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "matches the override repositories/override/sample-service/CODEOWNERS of giantswarm/github", res.Step(StepCodeowners).Summary)
			},
		},
		{
			name: "codeowners: a file differing from the override gets the override verbatim", step: StepCodeowners,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).files["CODEOWNERS"] = reposetup.Codeowners(team)
				h.codeownersOverride = []byte(testCodeownersOverride)
			},
			wantCheck: VerdictDrift, wantChange: "open a pull request setting CODEOWNERS to the override repositories/override/sample-service/CODEOWNERS of giantswarm/github",
			wantAfter: VerdictDrift, // the pull request awaits its merge; nothing else happens
			verify: func(t *testing.T, h *harness, res *Result) {
				r := h.repo()
				require.Len(t, r.prs, 1)
				require.Equal(t, testCodeownersOverride, r.branchFiles[codeownersBranch]["CODEOWNERS"], "the override verbatim")
				require.Equal(t, "chore: set CODEOWNERS to its override in giantswarm/github", r.headSubject(codeownersBranch), "a conventional commit for auto-release")
				require.Equal(t, "chore: set CODEOWNERS to its override in giantswarm/github", r.prs[0].GetTitle())
				require.Contains(t, r.prs[0].GetBody(), "repositories/override/sample-service/CODEOWNERS of giantswarm/github")
				require.Equal(t, reposetup.Codeowners(team), r.files["CODEOWNERS"], "main is untouched")
				findings := res.Step(StepCodeowners).Findings
				require.Equal(t, []FindingKind{FindingPendingPullRequest}, kinds(findings))
				require.Contains(t, findings[0].Message, "sets CODEOWNERS to the override repositories/override/sample-service/CODEOWNERS")
			},
		},
		{
			name: "metadata: description and visibility follow the declaration", step: StepMetadata,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.description, r.private = "", true
			},
			wantCheck: VerdictDrift, wantChange: `description "" → "A sample service", visibility → public`,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, "A sample service", h.repo().description)
				require.False(t, h.repo().private)
			},
		},
		{
			name: "metadata: the topics follow the declaration, whatever their order on GitHub", step: StepMetadata,
			entry: entryYAML + "  topics: [agentlab, fixture]\n",
			seed: func(h *harness) {
				seedMetadata(h, []string{"stale"})
			},
			wantCheck: VerdictDrift, wantChange: "topics [stale] → [agentlab, fixture]",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, []string{"agentlab", "fixture"}, h.repo().topics)
			},
		},
		{
			name: "metadata: topics declared in another order are as declared", step: StepMetadata,
			entry: entryYAML + "  topics: [fixture, agentlab]\n",
			seed: func(h *harness) {
				seedMetadata(h, []string{"agentlab", "fixture"})
			},
			wantCheck: VerdictOK,
		},
		{
			name: "metadata: an empty topics list removes every topic", step: StepMetadata,
			entry: entryYAML + "  topics: []\n",
			seed: func(h *harness) {
				seedMetadata(h, []string{"stale"})
			},
			wantCheck: VerdictDrift, wantChange: "topics [stale] → []",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Empty(t, h.repo().topics)
			},
		},
		{
			name: "metadata: an entry without topics leaves them alone", step: StepMetadata,
			seed: func(h *harness) {
				seedMetadata(h, []string{"kept"})
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, []string{"kept"}, h.repo().topics)
			},
		},
		{
			name: "labels: a missing owning-team label is created in the default colour", step: StepLabels,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).labels = map[string]*github.Label{}
			},
			wantCheck: VerdictDrift, wantChange: "create the label team/bumblebee (#ededed)",
			verify: func(t *testing.T, h *harness, _ *Result) {
				l := h.repo().labels["team/bumblebee"]
				require.NotNil(t, l)
				require.Equal(t, DefaultLabelColor, l.Color)
				require.Empty(t, l.GetDescription())
			},
		},
		{
			name: "labels: an existing owning-team label is left as it is, whatever its case, colour and description", step: StepLabels,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).labels = map[string]*github.Label{
					"team/bumblebee": {Name: "Team/Bumblebee", Color: "1D76DB", Description: new("Team Bumblebee")},
				}
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "has team/bumblebee", res.Step(StepLabels).Summary)
				require.Equal(t, "1D76DB", h.repo().labels["team/bumblebee"].Color)
				require.Empty(t, h.mutations())
			},
		},
		{
			// CircleCI's deploy key goes on GitHub — CircleCI's own way to
			// stop an OAuth project it refuses to stop for a renamed
			// repository — and its webhook stays, a hook an archived
			// repository sends nothing through. The engine holds no CircleCI
			// identity: it unfollows nothing and makes no CircleCI request.
			name: "lifecycle: archived deletes CircleCI's deploy key on GitHub, then archives, with no CircleCI request", step: StepLifecycle, entry: archivedEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.deployKeys = []*github.Key{circleCIDeployKey()}
				r.hooks = []*github.Hook{{ID: new(int64(683979223)), Name: new("web"), Active: new(true), Events: []string{"push"}, Config: &github.HookConfig{URL: new("https://circleci.com/hooks/github")}}}
				h.cc.addOAuth(owner, name)
			},
			wantCheck: VerdictDrift, wantChange: "delete CircleCI's deploy key 164277013; archive on GitHub",
			verify: func(t *testing.T, h *harness, repair *Result) {
				require.Equal(t, []string{"delete CircleCI's deploy key 164277013", "archive on GitHub"}, repair.Step(StepLifecycle).Changes, "no unfollow")
				require.Equal(t, "archived", repair.Step(StepLifecycle).Summary)
				require.True(t, h.repo().archived)
				require.Empty(t, h.repo().deployKeys, "CircleCI can no longer check the repository out")
				require.Len(t, h.repo().hooks, 1, "the webhook stays")
				require.Empty(t, h.cc.mutations, "the project is left as it is on CircleCI")
				require.Zero(t, h.runner.CircleCIRequests.Count(), "no CircleCI request at all")
			},
		},
		{
			// The case of devctl#2409: CircleCI refused "stop building" for a
			// renamed repository even from an admin. The key goes under the
			// repository's new slug on GitHub; the push-only identity needs
			// no admin grant and CircleCI is not asked anything.
			name: "lifecycle: a renamed repository archived by a push-only identity loses CircleCI's deploy key without an admin grant", step: StepLifecycle, entry: archivedEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name+"-v2")
				h.gh.redirects[owner+"/"+name] = owner + "/" + name + "-v2"
				h.gh.permission = "write"
				r.deployKeys = []*github.Key{circleCIDeployKey()}
			},
			wantCheck: VerdictDrift, wantChange: "delete CircleCI's deploy key 164277013; archive on GitHub",
			verify: func(t *testing.T, h *harness, _ *Result) {
				r := h.gh.repos[owner+"/"+name+"-v2"]
				require.True(t, r.archived)
				require.Empty(t, r.deployKeys)
				require.Empty(t, h.cc.mutations, "CircleCI is asked nothing")
				require.Equal(t, []string{
					"DELETE /repos/giantswarm/sample-service-v2/keys/164277013",
					"PATCH /repos/giantswarm/sample-service-v2",
				}, h.gh.mutations, "no admin grant; the key goes, then the archive")
			},
		},
		{
			// An archive left half done — archived, CircleCI's key still on
			// the repository — is finished on the next run.
			name: "lifecycle: an archived repository still carrying CircleCI's deploy key loses it", step: StepLifecycle, entry: archivedEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.archived = true
				r.deployKeys = []*github.Key{circleCIDeployKey()}
			},
			wantCheck: VerdictDrift, wantChange: "delete CircleCI's deploy key 164277013",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.True(t, h.repo().archived)
				require.Empty(t, h.repo().deployKeys)
				require.Equal(t, []string{"DELETE /repos/giantswarm/sample-service/keys/164277013"}, h.gh.mutations)
			},
		},
		{
			name: "lifecycle: an archived repository CircleCI has left is left alone, another deploy key too", step: StepLifecycle, entry: archivedEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.archived = true
				r.deployKeys = []*github.Key{{ID: new(int64(7)), Title: new("argocd"), ReadOnly: new(true)}}
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, repair *Result) {
				require.Equal(t, "archived", repair.Step(StepLifecycle).Summary, "no note about CircleCI")
				require.Len(t, h.repo().deployKeys, 1)
			},
		},
		{
			// An identity without the administration permission reads no
			// deploy keys (404 to a user, 403 to an App): the archive goes
			// on silently, and the reconciler's run deletes the key.
			name: "lifecycle: deploy keys the identity cannot read are skipped silently, the archive proceeds", step: StepLifecycle, entry: archivedEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.deployKeys = []*github.Key{circleCIDeployKey()}
				r.keysStatus = 403
			},
			wantCheck: VerdictDrift, wantChange: "archive on GitHub",
			verify: func(t *testing.T, h *harness, repair *Result) {
				require.True(t, h.repo().archived)
				require.Equal(t, []string{"archive on GitHub"}, repair.Step(StepLifecycle).Changes)
				require.Empty(t, repair.Findings(), "unreadable keys are no finding")
				require.Equal(t, []string{"PATCH /repos/giantswarm/sample-service"}, h.gh.mutations)
			},
		},
		{
			name: "lifecycle: deleted deletes on GitHub alone, without an admin grant or a CircleCI request; the next run finds the record", step: StepLifecycle, entry: deletedEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.gh.permission = "write"
				h.cc.addApp(owner, name)
			},
			wantCheck: VerdictDrift, wantChange: "delete on GitHub",
			// The second repair finds no repository: the create step reads the
			// declaration as the record and the lifecycle step is skipped on it.
			wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, repair *Result) {
				_, exists := h.gh.repos[owner+"/"+name]
				require.False(t, exists, "deleted on GitHub")
				require.Equal(t, []string{"delete on GitHub"}, repair.Step(StepLifecycle).Changes, "no unfollow")
				require.Empty(t, h.cc.mutations, "the project stays on CircleCI untouched")
				require.Zero(t, h.runner.CircleCIRequests.Count(), "no CircleCI request at all")
				require.Equal(t, []string{"DELETE /repos/giantswarm/sample-service"}, h.gh.mutations, "no admin grant, no key deleted before the repository")
				require.Equal(t, "deleted", repair.Step(StepLifecycle).Summary)
			},
		},
		{
			name: "lifecycle: deleted on a repository that is archived on GitHub deletes it", step: StepLifecycle, entry: deletedEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).archived = true },
			wantCheck: VerdictDrift, wantChange: "delete on GitHub", wantAfter: VerdictSkipped,
		},
		{
			name: "lifecycle: archived on GitHub without the lifecycle is reported", step: StepLifecycle,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).archived = true },
			wantCheck: VerdictReported, wantFinding: FindingArchivedUndeclared, wantAfter: VerdictReported,
		},
		{
			// The way back: the change took lifecycle: archived from the entry.
			name: "lifecycle: a change that took lifecycle: archived from the entry unarchives the repository", step: StepLifecycle, unarchived: true,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).archived = true },
			wantCheck: VerdictDrift, wantChange: "unarchive on GitHub",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.False(t, h.repo().archived)
				require.Equal(t, []string{"PATCH /repos/giantswarm/sample-service"}, h.gh.mutations)
			},
		},
		{
			name: "lifecycle: an entry declared archived is archived, whatever the change says", step: StepLifecycle, entry: archivedEntryYAML, unarchived: true,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).archived = true },
			wantCheck: VerdictOK,
		},
		{
			name: "lifecycle: archiving reports the pull requests still open", step: StepLifecycle, entry: archivedEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).prs = []*github.PullRequest{
					{Number: new(7), State: new("open"), Head: &github.PullRequestBranch{Ref: new("feature")}},
					{Number: new(9), State: new("open"), Head: &github.PullRequestBranch{Ref: new("renovate/x")}},
				}
			},
			wantCheck: VerdictDrift, wantChange: "archive on GitHub", wantFinding: FindingOpenPullRequests,
			verify: func(t *testing.T, h *harness, repair *Result) {
				require.True(t, h.repo().archived)
				sr := repair.Step(StepLifecycle)
				require.Len(t, sr.Findings, 1)
				require.Contains(t, sr.Findings[0].Message, "#7, #9")
				require.Contains(t, sr.Findings[0].Fix, "devctl repo update sample-service --unset lifecycle")
				require.True(t, sr.Converges(), "the finding is advisory: the archive is done as declared")
			},
		},
		{
			name: "catalog: a missing component dispatches the regeneration", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalog(false, false)
			},
			wantCheck: VerdictDrift, wantChange: "dispatch update-devportal-catalog.yaml in giantswarm/github (force)",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, []string{"update-devportal-catalog.yaml"}, h.gh.dispatches)
			},
		},
		{
			name: "catalog: a missing mapping dispatches the mapping run for the repository", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalog(true, false)
			},
			wantCheck: VerdictDrift, wantChange: "dispatch apps-to-teams-mapping.yaml in giantswarm/github for sample-service",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, []string{"apps-to-teams-mapping.yaml"}, h.gh.dispatches)
			},
		},
		{
			name: "catalog: a component without a chart has nothing to map", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalogCharts(true, false, nil)
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.gh.dispatches)
				require.Equal(t, "in the catalog; no public chart to map", res.Step(StepCatalog).Summary)
			},
		},
		{
			name: "catalog: a private chart has nothing to map", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalogCharts(true, false, []string{"gsociprivate.azurecr.io/charts/giantswarm/" + name})
			},
			wantCheck: VerdictOK,
			verify:    func(t *testing.T, h *harness, _ *Result) { require.Empty(t, h.gh.dispatches) },
		},
		{
			name: "catalog: a private repository's chart is no chart to map, whatever its registry", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalogCharts(false, false, nil)
				h.gh.repos[owner+"/github"].files["catalog/components.yaml"] += taggedComponentYAML(name, []string{"helmchart", "helmchart-deployable", "private"}, name)
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.gh.dispatches, "the mapping's generator skips a private component; a dispatch for it changes nothing")
				require.Equal(t, "in the catalog; no public chart to map", res.Step(StepCatalog).Summary)
			},
		},
		{
			name: "catalog: a chart that is not deployable is no chart to map", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalogCharts(false, false, nil)
				h.gh.repos[owner+"/github"].files["catalog/components.yaml"] += taggedComponentYAML(name, []string{"helmchart"}, name)
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.gh.dispatches)
				require.Equal(t, "in the catalog; no public chart to map", res.Step(StepCatalog).Summary)
			},
		},
		{
			name: "catalog: a template's placeholder chart is no chart to map", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalogCharts(true, false, []string{"{MCP-NAME}"})
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.gh.dispatches, "the mapping drops a placeholder; a dispatch for it changes nothing")
				require.Equal(t, "in the catalog; no public chart to map", res.Step(StepCatalog).Summary)
			},
		},
		{
			name: "catalog: a placeholder beside a chart is left out of the mapping check", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalogCharts(true, false, []string{"sample-chart", "{APP-NAME}"})
				h.gh.repos[owner+"/management-cluster-bases"].files["bases/apps-to-teams-mapping/configmap.yaml"] += "  sample-chart: bumblebee\n"
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.gh.dispatches)
				require.Equal(t, "in the catalog and the mapping (sample-chart)", res.Step(StepCatalog).Summary)
			},
		},
		{
			name: "catalog: the mapping is matched by chart name, not repository name", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalogCharts(true, true, []string{"sample-chart", "sample-crds"})
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.gh.dispatches)
				require.Equal(t, "in the catalog and the mapping (sample-chart, sample-crds)", res.Step(StepCatalog).Summary)
			},
		},
		{
			name: "catalog: one chart of two missing from the mapping dispatches the mapping run once", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalogCharts(true, false, []string{"sample-chart", "sample-crds"})
				h.gh.repos[owner+"/management-cluster-bases"].files["bases/apps-to-teams-mapping/configmap.yaml"] += "  sample-chart: bumblebee\n"
			},
			wantCheck: VerdictDrift, wantChange: "the mapping lacks sample-crds",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, []string{"apps-to-teams-mapping.yaml"}, h.gh.dispatches)
			},
		},
		{
			name: "catalog: a run in progress is waited for, not dispatched again", step: StepCatalog,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.seedCatalog(false, false)
				h.gh.runs[owner+"/github/update-devportal-catalog.yaml"] = []string{"completed", "in_progress"}
			},
			wantCheck: VerdictDrift, wantRepair: VerdictDrift, wantAfter: VerdictDrift,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.gh.dispatches)
				require.Contains(t, res.Step(StepCatalog).Summary, "run #2 in_progress")
			},
		},
		{
			name: "catalog: a private repository is looked up as the App and dispatched as the run", step: StepCatalog,
			entry: privateEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).private = true
				h.seedCatalog(false, false)
				h.dispatchAsRun()
			},
			wantCheck: VerdictDrift, wantChange: "dispatch update-devportal-catalog.yaml in giantswarm/github (force)",
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, []string{"update-devportal-catalog.yaml"}, h.gh.dispatches)
				require.Equal(t, []string{"run-token"}, h.gh.dispatchedBy, "the dispatch carries the run's token")
				require.Equal(t, VerdictRepaired, res.Step(StepCatalog).Verdict)
			},
		},
		{
			name: "catalog: a private repository in the catalog reaches its verdict with the run's token dispatching", step: StepCatalog,
			entry: privateEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).private = true
				h.seedCatalogCharts(true, false, []string{"gsociprivate.azurecr.io/charts/giantswarm/" + name})
				h.dispatchAsRun()
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, h.gh.dispatches)
				require.Equal(t, "in the catalog; no public chart to map", res.Step(StepCatalog).Summary)
			},
		},
		{
			// The release step reads GitHub statuses of the tag's commit and
			// never CircleCI. A tag whose commit carries no status of a build
			// started after the release, beyond the grace, is a missed
			// build: reported in every mode, never triggered.
			name: "release: a tag without a build is a missed build, reported and never triggered", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
			},
			wantCheck: VerdictReported, wantFinding: FindingMissedTagBuild, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				f := res.Findings()[0]
				require.Equal(t, "release v0.1.0 of "+owner+"/"+name+" carries no CircleCI status of a build started after the release: nothing was built or published for the tag", f.Message)
				require.Contains(t, f.Fix, "`devctl repo follow-circleci "+owner+"/"+name+"`")
				require.Equal(t, "release v0.1.0: no tag build", res.Step(StepRelease).Summary)
				require.Empty(t, h.cc.mutations, "no pipeline is triggered")
				require.Zero(t, h.runner.CircleCIRequests.Count(), "the release step reads GitHub, not CircleCI")
			},
		},
		{
			// CircleCI posts the first status within seconds of the tag push:
			// a release published half a minute ago may simply not have one
			// yet, which is neither a finding nor a build.
			name: "release: a tag published within the grace without a status yet reports nothing", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-30*time.Second)
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Empty(t, res.Findings())
				require.Regexp(t, `^release v0\.1\.0: published \d+s ago, no status of its build yet$`, res.Step(StepRelease).Summary)
				require.Empty(t, h.cc.mutations)
			},
		},
		{
			// The creation's first release (the scaffold's tag) is no
			// different: no tag build after the grace is reported as missed,
			// whatever the change, and nothing is triggered.
			name: "release: the scaffold tag of a creation without a build is reported, never triggered", step: StepRelease, added: true,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt, r.releaseSubject = "v0.1.0", time.Now().Add(-time.Hour), scaffoldSubject
			},
			wantCheck: VerdictReported, wantFinding: FindingMissedTagBuild, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Empty(t, h.cc.mutations, "no pipeline is triggered")
				require.Empty(t, h.mutations())
			},
		},
		{
			name: "release: a green tag build reads built", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.release, r.releaseAt = "v0.1.0", published
				r.addTagBuild("v0.1.0", "wf-tag", published, jobState{"go-build", "success"}, jobState{"push-to-registries-release", "success"})
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "release v0.1.0 built: go-build, push-to-registries-release succeeded", res.Step(StepRelease).Summary)
				require.Empty(t, res.Findings())
				require.Empty(t, h.cc.mutations)
			},
		},
		{
			// The real shape (release-automation-sandbox): the release was
			// published 11:20:53Z. The branch build of the same commit started
			// at 11:20:41Z, before the release, and its go-build failed at
			// 11:21:34Z, after the tag's own go-build went green at 11:21:30Z.
			// The commit's statuses carry both; only the tag's workflow
			// counts, so the newest go-build is not the branch's failure.
			name: "release: a branch build of the same commit that started before the release is ignored", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				ts := func(clock string) time.Time {
					at, err := time.Parse(time.RFC3339, "2026-10-30T"+clock+"Z")
					require.NoError(t, err)
					return at
				}
				r.release, r.releaseAt = "v0.1.0", ts("11:20:53")
				r.addStatus("v0.1.0", "go-build", "pending", "wf-branch", CircleCIAppLogin, ts("11:20:41"))
				r.addStatus("v0.1.0", "go-build", "failure", "wf-branch", CircleCIAppLogin, ts("11:21:34"))
				r.addStatus("v0.1.0", "go-build", "pending", "wf-tag", CircleCIAppLogin, ts("11:20:55"))
				r.addStatus("v0.1.0", "go-build", "success", "wf-tag", CircleCIAppLogin, ts("11:21:30"))
				r.addStatus("v0.1.0", "push-to-registries-release", "pending", "wf-tag", CircleCIAppLogin, ts("11:20:56"))
				r.addStatus("v0.1.0", "push-to-registries-release", "success", "wf-tag", CircleCIAppLogin, ts("11:22:10"))
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "release v0.1.0 built: go-build, push-to-registries-release succeeded", res.Step(StepRelease).Summary)
				require.Empty(t, res.Findings())
			},
		},
		{
			// A commit whose only statuses are the branch build's, started
			// before the release, has no tag build: it was never built.
			name: "release: only a branch build that started before the release is a missed tag build", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.release, r.releaseAt = "v0.1.0", published
				r.addStatus("v0.1.0", "go-build", "success", "wf-branch", CircleCIAppLogin, published.Add(-5*time.Minute))
			},
			wantCheck: VerdictReported, wantFinding: FindingMissedTagBuild, wantAfter: VerdictReported,
		},
		{
			name: "release: a red tag build is reported, the fix a rerun from failed", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.release, r.releaseAt = "v0.1.0", published
				r.addTagBuild("v0.1.0", "wf-tag", published, jobState{"go-build", "success"}, jobState{"push-to-registries-release", "failure"})
			},
			wantCheck: VerdictReported, wantFinding: FindingRedRelease, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				f := res.Findings()[0]
				require.Contains(t, f.Message, "push-to-registries-release (failure)")
				require.Contains(t, f.Fix, "rerun the failed workflow from failed")
				require.Contains(t, f.Fix, "devctl release wait "+owner+"/"+name+" v0.1.0")
				require.Equal(t, "release v0.1.0: red", res.Step(StepRelease).Summary)
				require.Empty(t, h.cc.mutations, "nothing is rerun by the engine")
			},
		},
		{
			// An error state is red like a failure.
			name: "release: a job in error state is red", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.release, r.releaseAt = "v0.1.0", published
				r.addTagBuild("v0.1.0", "wf-tag", published, jobState{"go-build", "error"})
			},
			wantCheck: VerdictReported, wantFinding: FindingRedRelease, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Contains(t, res.Findings()[0].Message, "go-build (error)")
			},
		},
		{
			// auto-release creates the GitHub release before the tag
			// pipeline runs; the pipeline's publish-github-release then
			// fails on the release being there, after every artifact was
			// pushed. The release is published, not red.
			name: "release: a tag build red only in publish-github-release reads built", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.release, r.releaseAt = "v0.1.0", published
				r.addTagBuild("v0.1.0", "wf-tag", published, jobState{"go-build", "success"}, jobState{"push-to-registries-release", "success"}, jobState{GitHubReleaseJob, "failure"})
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Empty(t, res.Findings())
				require.Equal(t, "release v0.1.0 built: go-build, push-to-registries-release succeeded; publish-github-release failed, the GitHub release it publishes exists", res.Step(StepRelease).Summary)
			},
		},
		{
			// An artifact job red beside publish-github-release leaves the
			// artifact unpublished: still red, the artifact job named alone.
			name: "release: a tag build red in an artifact job and publish-github-release is red", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.release, r.releaseAt = "v0.1.0", published
				r.addTagBuild("v0.1.0", "wf-tag", published, jobState{"go-build", "success"}, jobState{"push-to-registries-release", "failure"}, jobState{GitHubReleaseJob, "failure"})
			},
			wantCheck: VerdictReported, wantFinding: FindingRedRelease, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Contains(t, res.Findings()[0].Message, "push-to-registries-release")
				require.NotContains(t, res.Findings()[0].Message, GitHubReleaseJob)
			},
		},
		{
			// A rerun from failed is a second workflow of the tag's build;
			// the run it replaced keeps its failed status. The newest status
			// of each context decides, so the revived tag reads built.
			name: "release: a red tag build rerun green reads built", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.release, r.releaseAt = "v0.1.0", published
				r.addTagBuild("v0.1.0", "wf-tag", published, jobState{"go-build", "success"}, jobState{"push-to-registries-release", "failure"})
				r.addStatus("v0.1.0", "push-to-registries-release", "success", "wf-rerun", CircleCIAppLogin, published.Add(10*time.Minute))
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Contains(t, res.Step(StepRelease).Summary, "release v0.1.0 built")
				require.Empty(t, res.Findings())
			},
		},
		{
			// A job still running: the release is building, which is no
			// finding and no built release.
			name: "release: a pending job reads building", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.release, r.releaseAt = "v0.1.0", published
				r.addTagBuild("v0.1.0", "wf-tag", published, jobState{"go-build", "success"}, jobState{"push-to-registries-release", "pending"})
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "release v0.1.0: building: push-to-registries-release", res.Step(StepRelease).Summary)
				require.Empty(t, res.Findings())
			},
		},
		{
			// The reconciler holds no CircleCI client, and the release step
			// does not need one: it is never skipped for that reason.
			name: "release: without a CircleCI client the release is verified from the statuses", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.release, r.releaseAt = "v0.1.0", published
				r.addTagBuild("v0.1.0", "wf-tag", published, jobState{"go-build", "success"})
				h.runner.CircleCI = nil
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, VerdictOK, res.Step(StepRelease).Verdict)
				require.Equal(t, "release v0.1.0 built: go-build succeeded", res.Step(StepRelease).Summary)
			},
		},
		{
			name: "release: without a CircleCI client a missed build is still reported", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				h.runner.CircleCI = nil
			},
			wantCheck: VerdictReported, wantFinding: FindingMissedTagBuild, wantAfter: VerdictReported,
		},
		{
			// A repository tagging per component (base/v0.1.0) releases
			// outside the flow the step verifies: auto-release cuts vX.Y.Z
			// and the generated pipeline builds /^v.*/. Its latest release
			// is not held against CircleCI: the step is skipped naming the
			// tag, no status is read and nothing is reported.
			name: "release: a release whose tag is not vX.Y.Z is not verified", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "base/v0.1.0", time.Now().Add(-time.Hour)
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "release base/v0.1.0: not a vX.Y.Z tag, not verified", res.Step(StepRelease).Summary)
				require.Empty(t, res.Findings())
				require.Zero(t, h.gh.reads("/repos/"+owner+"/"+name+"/commits/base/v0.1.0/statuses"), "no status is read")
				require.Empty(t, h.cc.mutations)
			},
		},
		{
			// A multi-image repository releases one line per image. GitHub's
			// latest release is the newest of any line; the step verifies the
			// newest published release of each line instead, older ones,
			// drafts, pre-releases and tags outside the grammar left out.
			name: "release: a multi-image repository verifies the newest release of each image line", step: StepRelease, entry: multiImageEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				now := time.Now()
				r.release, r.releaseAt = "python/v0.2.0", now.Add(-time.Hour)
				r.releases = []fakeRelease{
					{tag: "go/v0.3.0-rc.1", at: now.Add(-50 * time.Minute), prerelease: true},
					{tag: "node/v0.3.0", at: now.Add(-55 * time.Minute), draft: true},
					{tag: "python/v0.2.0", at: now.Add(-40 * time.Minute)},
					{tag: "node/v0.2.0", at: now.Add(-time.Hour)},
					{tag: "go/v0.10.0", at: now.Add(-time.Hour)},
					{tag: "go/v0.9.0", at: now.Add(-2 * time.Hour)},
					{tag: "v1.0.0", at: now.Add(-3 * time.Hour)},
				}
				r.releasesPage = 2
				for _, rel := range r.releases[2:5] {
					r.addTagBuild(rel.tag, "wf-"+rel.tag, rel.at, jobState{"build-image", "success"})
				}
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Empty(t, res.Findings())
				summary := res.Step(StepRelease).Summary
				// python/v0.2.0 is GitHub's latest, the newest published
				// release: it leads, the others follow by image name.
				require.Equal(t, "release python/v0.2.0 built: build-image succeeded; release go/v0.10.0 built: build-image succeeded; release node/v0.2.0 built: build-image succeeded", summary)
			},
		},
		{
			// Each line is held against its own tag build: a red release
			// of one image is reported beside the built ones. Release wait
			// follows vX.Y.Z tags only, so the fix does not name it.
			name: "release: a multi-image repository reports a red image release", step: StepRelease, entry: multiImageEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				published := time.Now().Add(-time.Hour)
				r.releases = []fakeRelease{{tag: "node/v0.2.0", at: published}, {tag: "go/v0.2.0", at: published}}
				r.addTagBuild("go/v0.2.0", "wf-go", published, jobState{"build-image", "success"})
				r.addTagBuild("node/v0.2.0", "wf-node", published, jobState{"build-image", "failure"})
			},
			wantCheck: VerdictReported, wantFinding: FindingRedRelease, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Len(t, res.Findings(), 1)
				f := res.Findings()[0]
				require.Contains(t, f.Message, "release node/v0.2.0 of "+owner+"/"+name+" is red")
				require.NotContains(t, f.Fix, "devctl release wait")
				require.Contains(t, res.Step(StepRelease).Summary, "release go/v0.2.0 built")
				require.Contains(t, res.Step(StepRelease).Summary, "release node/v0.2.0: red")
			},
		},
		{
			// An image tag nobody built is a missed build of that line.
			name: "release: a multi-image repository reports an image release without a build", step: StepRelease, entry: multiImageEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.releases = []fakeRelease{{tag: "go/v0.2.0", at: time.Now().Add(-time.Hour)}}
			},
			wantCheck: VerdictReported, wantFinding: FindingMissedTagBuild, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Contains(t, res.Findings()[0].Message, "release go/v0.2.0 of "+owner+"/"+name+" carries no CircleCI status")
				require.Empty(t, h.cc.mutations, "no pipeline is triggered")
			},
		},
		{
			name: "release: a multi-image repository without an image release has none yet", step: StepRelease, entry: multiImageEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.releases = []fakeRelease{{tag: "v0.1.0", at: time.Now().Add(-time.Hour)}}
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "no <name>/vX.Y.Z release yet", res.Step(StepRelease).Summary)
			},
		},
		{
			// giantswarm-repo-manager runs the engine without a CircleCI
			// client: GitHub's latest release leads the summary, the one
			// its inventory reads, and each line is verified from statuses.
			name: "release: a multi-image repository without a CircleCI client is verified from statuses, GitHub's latest first", step: StepRelease, entry: multiImageEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				at := time.Now().Add(-time.Hour)
				// Two releases of one commit tie on its date: GitHub's
				// answer, not the list, names node/v0.2.1 its latest.
				r.release, r.releaseAt = "node/v0.2.1", at
				r.releases = []fakeRelease{{tag: "github/v0.1.0", at: at}, {tag: "node/v0.2.1", at: at}}
				r.addTagBuild("github/v0.1.0", "wf-github", at, jobState{"build-image", "success"})
				r.addTagBuild("node/v0.2.1", "wf-node", at, jobState{"build-image", "success"})
				h.runner.CircleCI = nil
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, VerdictOK, res.Step(StepRelease).Verdict, "never skipped for the missing client")
				require.Equal(t, "release node/v0.2.1 built: build-image succeeded; release github/v0.1.0 built: build-image succeeded", res.Step(StepRelease).Summary)
			},
		},
		{
			name: "settings: a customer repository's default branch is never renamed", step: StepSettings, entry: customerEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).defaultBranch = "master" },
			wantCheck: VerdictOK,
			verify:    func(t *testing.T, h *harness, _ *Result) { require.Equal(t, "master", h.repo().defaultBranch) },
		},
		{
			name: "protection: flavour customer keeps the customer's protection", step: StepProtection, entry: customerEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).protection = &fakeProtection{reviews: 2, enforceAdmins: false, strict: true, checks: []string{"customer/build"}}
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, &fakeProtection{reviews: 2, enforceAdmins: false, strict: true, checks: []string{"customer/build"}}, h.repo().protection)
			},
		},
		{
			name: "circleci: flavour customer is not followed", step: StepCircleCI, entry: customerEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name) },
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) { require.Empty(t, h.cc.projects, "no follow, no deploy key") },
		},
		{
			name: "codeowners: flavour customer gets no CODEOWNERS pull request", step: StepCodeowners, entry: customerEntryYAML,
			seed:      func(h *harness) { delete(h.gh.addRepo(owner, name).files, "CODEOWNERS") },
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) { require.Empty(t, h.repo().prs) },
		},
		{
			name: "release: flavour customer has no pipeline of ours to verify", step: StepRelease, entry: customerEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name) },
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
		},
		{
			name: "settings: an app catalog's default branch is never renamed", step: StepSettings, entry: appCatalogEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).defaultBranch = "master" },
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "master", h.repo().defaultBranch)
				require.Empty(t, res.Step(StepSettings).Changes)
			},
		},
		{
			name: "protection: an app catalog's own default branch is the one protected", step: StepProtection, entry: appCatalogEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.defaultBranch = "master"
				r.statuses = []string{ctxGoBuild}
				r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID), adminBypass(), teamBypass(testTeamID))
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, `master: ruleset "devctl: default branch"; required: `+ctxGoBuild, res.Step(StepProtection).Summary)
				require.Equal(t, "master", h.repo().defaultBranch)
			},
		},
		{
			name: "settings: a fork line stays on its declared default branch", step: StepSettings, entry: forkEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).forkLine() },
			wantCheck: VerdictOK,
			verify:    func(t *testing.T, h *harness, _ *Result) { require.Equal(t, "giantswarm", h.repo().defaultBranch) },
		},
		{
			name: "settings: a repository off its declared default branch is renamed to it", step: StepSettings, entry: forkEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).allowRebase = true },
			wantCheck: VerdictDrift, wantChange: `default branch "main" → "giantswarm"`,
			verify: func(t *testing.T, h *harness, _ *Result) { require.Equal(t, "giantswarm", h.repo().defaultBranch) },
		},
		{
			// A fork registered with the upstream mirror on main and the
			// consumed branch beside it: GitHub refuses to rename main onto
			// the existing branch, so the step switches the default to it and
			// main stays the mirror.
			name: "settings: a declared default branch that exists becomes the default", step: StepSettings, entry: forkEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.allowRebase, r.heads["giantswarm"] = true, "line"
			},
			wantCheck: VerdictDrift, wantChange: `default branch "main" → "giantswarm" (switch to the existing branch)`,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, "giantswarm", h.repo().defaultBranch)
				require.True(t, h.repo().hasBranch("main"), "the old default stays")
			},
		},
		{
			// The carried patches land by rebase merge, one upstream-ready
			// commit each, and a re-pin merges upstream's history: the
			// squash-only baseline would have GitHub refuse the line's merges.
			name: "settings: a fork line keeps rebase merges and merge commits", step: StepSettings, entry: forkEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).forkLine().allowMerge = true },
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.True(t, h.repo().allowRebase && h.repo().allowMerge, "the merge methods the line merges by stay")
			},
		},
		{
			name: "settings: the rest of the baseline applies to a fork line", step: StepSettings, entry: forkEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name).forkLine()
				r.allowMerge, r.hasWiki, r.allowAuto, r.squashTitle = true, true, false, "COMMIT_OR_PR_TITLE"
			},
			wantCheck:  VerdictDrift,
			wantChange: "settings: has_wiki true → false, allow_auto_merge false → true, squash_merge_commit_title COMMIT_OR_PR_TITLE → PR_TITLE",
			verify: func(t *testing.T, h *harness, _ *Result) {
				r := h.repo()
				require.True(t, r.allowRebase && r.allowMerge, "the merge methods are not in the change")
				require.True(t, r.allowAuto && !r.hasWiki)
				require.Equal(t, "PR_TITLE", r.squashTitle)
			},
		},
		{
			name: "settings: a fork line without rebase merges gets them", step: StepSettings, entry: forkEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).defaultBranch = "giantswarm" },
			wantCheck: VerdictDrift, wantChange: "settings: allow_rebase_merge false → true",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.True(t, h.repo().allowRebase)
				require.False(t, h.repo().allowMerge, "merge commits stay off when they are off")
			},
		},
		{
			name: "settings: rebase merges and merge commits are drift off a fork line", step: StepSettings,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.allowMerge, r.allowRebase = true, true
			},
			wantCheck: VerdictDrift, wantChange: "settings: allow_merge_commit true → false, allow_rebase_merge true → false",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.False(t, h.repo().allowRebase || h.repo().allowMerge, "squash alone, as everywhere")
			},
		},
		{
			name: "protection: a fork line's declared default branch is protected", step: StepProtection, entry: forkEntryYAML,
			seed: func(h *harness) {
				h.runner.DevctlAppID = 0 // classic protection names the branch; the ruleset follows it (TestRunForkLineFollowsItsDeclaredBranch)
				h.gh.addRepo(owner, name).defaultBranch = "giantswarm"
			},
			wantCheck: VerdictDrift, wantChange: "protect giantswarm", wantFinding: FindingRulesetsNotEnabled, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, "giantswarm", h.repo().protected, "the declared branch is the protected one")
				require.NotNil(t, h.repo().protection)
			},
		},
		{
			name: "scaffold: flavour fork carries its upstream's tree", step: StepScaffold, entry: forkEntryYAML,
			seed:      func(h *harness) { h.gh.addRepo(owner, name).defaultBranch = "giantswarm" },
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
		},
		{
			name: "codeowners: flavour fork gets no CODEOWNERS pull request", step: StepCodeowners, entry: forkEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.defaultBranch = "giantswarm"
				delete(r.files, "CODEOWNERS")
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) { require.Empty(t, h.repo().prs) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yaml := tc.entry
			if yaml == "" {
				yaml = entryYAML
			}
			mode := reposetup.ModeCreate
			if tc.existing {
				mode = reposetup.ModeExisting
			}
			h := newHarnessMode(t, yaml, mode)
			h.unarchived = tc.unarchived
			if tc.seed != nil {
				tc.seed(h)
			}

			// The check reports the drift the fake presents and writes nothing.
			check := h.run(ModeCheck, tc.added, tc.step)
			sr := check.Step(tc.step)
			require.NotNil(t, sr)
			require.Equal(t, tc.wantCheck, sr.Verdict, "check: %+v", sr)
			if tc.wantChange != "" {
				require.Contains(t, strings.Join(sr.Changes, "; "), tc.wantChange)
			}
			if tc.wantFinding != "" {
				require.Contains(t, kinds(sr.Findings), tc.wantFinding)
			}
			for _, f := range sr.Findings {
				require.NotEmpty(t, f.Fix, "finding %s without a fix", f.Kind)
			}
			require.Empty(t, h.mutations(), "a check must not write")

			// The repair converges in one run.
			wantRepair := tc.wantRepair
			if wantRepair == "" {
				wantRepair = tc.wantCheck
				if tc.wantCheck == VerdictDrift {
					wantRepair = VerdictRepaired
				}
			}
			repair := h.run(ModeRepair, tc.added, tc.step)
			sr = repair.Step(tc.step)
			require.Equal(t, wantRepair, sr.Verdict, "repair: %+v", sr)
			if tc.wantCheck != VerdictDrift {
				require.Empty(t, h.mutations(), "nothing to repair, nothing written")
			}
			if tc.verify != nil {
				tc.verify(t, h, repair)
			}

			// The second run is a no-op.
			h.resetMutations()
			wantAfter := tc.wantAfter
			if wantAfter == "" {
				wantAfter = VerdictOK
			}
			again := h.run(ModeRepair, tc.added, tc.step)
			require.Equal(t, wantAfter, again.Step(tc.step).Verdict, "second run: %+v", again.Step(tc.step))
			require.Empty(t, h.mutations(), "the second run must change nothing")
		})
	}
}

// chartFilesAt is scaffoldFiles with the chart under helm/<chart> instead
// of helm/sample-service.
func chartFilesAt(chart string) map[string]string {
	files := map[string]string{}
	for p, c := range scaffoldFiles {
		files[strings.Replace(p, "helm/sample-service/", "helm/"+chart+"/", 1)] = c
	}
	return files
}

func kinds(findings []Finding) []FindingKind {
	out := make([]FindingKind, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
}

// TestRunMissedFirstReleaseIsReportedNeverBuilt: a chart repository (Go +
// app) created pull-request-last has its scaffold tagged before anyone
// follows the project on CircleCI. The engine holds no CircleCI identity and
// writes nothing there: whatever the grace says, it never triggers the tag.
// Past the two-minute grace the missing build is the finding
// missed-tag-build, whose fix is `devctl repo follow-circleci`, a person's
// command; within the grace the first status may not have been posted yet,
// and nothing is reported (devctl#2408 was the follow building this tag
// itself, which the engine no longer does).
func TestRunMissedFirstReleaseIsReportedNeverBuilt(t *testing.T) {
	steps := []Step{StepCircleCI, StepRelease}

	t.Run("past the grace", func(t *testing.T) {
		h := newHarnessMode(t, entryYAML, reposetup.ModeExisting)
		r := h.gh.addRepo(owner, name)
		r.release, r.releaseAt, r.releaseSubject = "v0.1.0", time.Now().Add(-10*time.Minute), scaffoldSubject

		for _, mode := range []Mode{ModeCheck, ModeRepair, ModeRepair} {
			res := h.run(mode, true, steps...)
			sr := res.Step(StepRelease)
			require.Equal(t, VerdictReported, sr.Verdict, "%s: %+v", mode, sr)
			require.Equal(t, []FindingKind{FindingMissedTagBuild}, kinds(sr.Findings))
			require.Contains(t, sr.Findings[0].Fix, "devctl repo follow-circleci "+owner+"/"+name)
			require.Empty(t, sr.Changes, "nothing is planned, nothing triggered")
			// The circleci step reports the missing pipeline, too: nobody
			// followed the project.
			require.Equal(t, []FindingKind{FindingCircleCIPipelineMissing}, kinds(res.Step(StepCircleCI).Findings))
			require.False(t, res.Converged)
		}
		require.Empty(t, h.mutations(), "no CircleCI write, no GitHub write")
		require.Empty(t, h.cc.projects, "no project is followed or created")
	})

	t.Run("within the grace", func(t *testing.T) {
		h := newHarnessMode(t, entryYAML, reposetup.ModeExisting)
		r := h.gh.addRepo(owner, name)
		r.release, r.releaseAt, r.releaseSubject = "v0.1.0", time.Now().Add(-30*time.Second), scaffoldSubject

		res := h.run(ModeRepair, true, StepRelease)
		sr := res.Step(StepRelease)
		require.Equal(t, VerdictOK, sr.Verdict, "%+v", sr)
		require.Empty(t, sr.Findings)
		require.Contains(t, sr.Summary, "ago, no status of its build yet")
		require.Empty(t, h.mutations())
	})

	t.Run("built once the first status lands", func(t *testing.T) {
		h := newHarnessMode(t, entryYAML, reposetup.ModeExisting)
		r := h.gh.addRepo(owner, name)
		published := time.Now().Add(-time.Hour)
		r.release, r.releaseAt, r.releaseSubject = "v0.1.0", published, scaffoldSubject
		r.addTagBuild("v0.1.0", "wf-tag", published, jobState{"go-build", "success"}, jobState{"push-to-registries-release", "success"})

		res := h.run(ModeRepair, true, StepRelease)
		require.Equal(t, VerdictOK, res.Step(StepRelease).Verdict, "%+v", res.Step(StepRelease))
		require.Contains(t, res.Step(StepRelease).Summary, "release v0.1.0 built")
		require.Empty(t, h.mutations())
	})
}

// TestRenovateWaitsForScaffold: on an empty repository the renovate step
// waits for the scaffold, which brings the configuration, instead of
// reporting a configuration nothing could have pushed yet.
func TestRenovateWaitsForScaffold(t *testing.T) {
	h := newHarness(t, entryYAML)
	h.gh.addRepo(owner, name).empty = true

	res := h.run(ModeCheck, false, StepScaffold, StepRenovate)
	sr := res.Step(StepRenovate)
	require.Equal(t, VerdictSkipped, sr.Verdict, "%+v", sr)
	require.Equal(t, "repository is empty: the scaffold comes first", sr.Summary)
}

// TestRunUnarchive takes a set-up repository through lifecycle: archived and
// back: the change that took the lifecycle from the entry unarchives the
// repository, and the same run reads its CircleCI pipeline, which the
// archive never touched; a run that does not carry the change, or leaves out
// the lifecycle step, leaves an archive alone.
func TestRunUnarchive(t *testing.T) {
	h := newHarness(t, entryYAML)
	h.seedCatalog(false, false)
	h.gh.installation.selection = "all"
	h.cc.addApp(owner, name) // a person ran `devctl repo follow-circleci`
	require.True(t, h.run(ModeRepair, true).Converged)

	// What the lifecycle step's archive left: CircleCI's deploy key gone
	// from GitHub, the repository archived, the CircleCI project untouched.
	h.repo().deployKeys = nil
	h.repo().archived = true
	h.resetMutations()

	reported := h.run(ModeRepair, false)
	require.Equal(t, []FindingKind{FindingArchivedUndeclared}, kinds(reported.Step(StepLifecycle).Findings))
	unselected := h.run(ModeRepair, false, StepSettings)
	require.Equal(t, VerdictSkipped, unselected.Step(StepSettings).Verdict)
	h.unarchived = true
	unselected = h.run(ModeRepair, false, StepSettings)
	require.Equal(t, VerdictSkipped, unselected.Step(StepSettings).Verdict, "no lifecycle step, no unarchive")
	require.Empty(t, h.mutations())

	check := h.run(ModeCheck, false)
	require.Equal(t, VerdictDrift, check.Step(StepLifecycle).Verdict)
	require.Equal(t, "archived on GitHub until the lifecycle step's unarchive", check.Step(StepSettings).Summary)
	require.True(t, check.Unarchived)
	require.Empty(t, h.mutations(), "a check must not write")

	repair := h.run(ModeRepair, false)
	for _, sr := range repair.Steps {
		require.NotEqual(t, VerdictFailed, sr.Verdict, "%s: %s", sr.Step, sr.Summary)
	}
	require.Equal(t, []string{"unarchive on GitHub"}, repair.Step(StepLifecycle).Changes)
	require.Equal(t, VerdictOK, repair.Step(StepCircleCI).Verdict, "%+v", repair.Step(StepCircleCI))
	require.False(t, h.repo().archived)
	require.Empty(t, h.cc.mutations, "the engine writes nothing on CircleCI")
	require.True(t, repair.Converged, "%+v", repair.Steps)

	h.resetMutations()
	h.unarchived = false
	again := h.run(ModeRepair, false)
	require.True(t, again.Converged, "%+v", again.Steps)
	require.Empty(t, h.mutations())
}

// TestRunFullRepositorySetUp runs every step over a repository that only
// exists as an added entry: the first repair sets up what the engine owns
// (bar the pull request and the findings for a person). The CircleCI
// pipeline is a person's: the circleci step reports it missing and creates
// nothing, so the run does not converge until someone ran `devctl repo
// follow-circleci`; with the pipeline there the second run converges and
// changes nothing.
func TestRunFullRepositorySetUp(t *testing.T) {
	h := newHarness(t, entryYAML)
	h.seedCatalog(false, false)
	h.gh.installation.selection = "all"

	first := h.run(ModeRepair, true)
	for _, sr := range first.Steps {
		require.NotEqual(t, VerdictFailed, sr.Verdict, "%s: %s", sr.Step, sr.Summary)
	}
	require.Equal(t, VerdictRepaired, first.Step(StepCreate).Verdict)
	require.Equal(t, VerdictRepaired, first.Step(StepScaffold).Verdict)
	require.Equal(t, VerdictRepaired, first.Step(StepProtection).Verdict)
	circle := first.Step(StepCircleCI)
	require.Equal(t, VerdictReported, circle.Verdict, "no CircleCI pipeline is created by the engine: %+v", circle)
	require.Equal(t, []FindingKind{FindingCircleCIPipelineMissing}, kinds(circle.Findings))
	require.Empty(t, circle.Changes)
	require.Empty(t, h.cc.mutations, "nothing is written on CircleCI")
	require.Empty(t, h.cc.projects, "no project is followed")
	require.Equal(t, VerdictRepaired, first.Step(StepCatalog).Verdict)
	require.Equal(t, VerdictOK, first.Step(StepCodeowners).Verdict, "the scaffold's CODEOWNERS names the team")
	require.Equal(t, VerdictOK, first.Step(StepRelease).Verdict, "no release yet")
	require.Empty(t, checkContexts(h.repo().ruleset(RulesetName)), "nothing has reported on a fresh repository, so nothing is required")
	require.Nil(t, h.repo().protection, "the protection is the ruleset, no classic protection")
	require.False(t, first.Converged, "the missing CircleCI pipeline is a finding for a person")

	h.cc.addApp(owner, name) // a person ran `devctl repo follow-circleci`
	h.resetMutations()
	second := h.run(ModeRepair, true)
	require.True(t, second.Converged, "%+v", second.Steps)
	require.Empty(t, h.mutations())
	for _, sr := range second.Steps {
		require.Contains(t, []Verdict{VerdictOK, VerdictReported}, sr.Verdict, "%s: %+v", sr.Step, sr)
	}
	require.Equal(t, []FindingKind{FindingDefaultIcon}, kinds(second.Findings()), "the default icon alone: Renovate's first run is not due on a repository created minutes ago")
	require.Contains(t, second.Step(StepRenovate).Summary, "none due", "%+v", second.Step(StepRenovate))

	data, err := json.Marshal(second)
	require.NoError(t, err)
	require.Contains(t, string(data), `"repository":"giantswarm/sample-service"`)

	// The budget: a check of the converged repository, every step, costs at
	// most twenty-four GitHub requests, and the count is the fake's own.
	before := len(h.gets())
	check := h.run(ModeCheck, false)
	require.True(t, check.Converged, "%+v", check.Steps)
	gets := h.gets()[before:]
	require.Equal(t, len(gets), check.Requests.GitHub, "the counter and the fake agree")
	require.LessOrEqual(t, check.Requests.GitHub, 24, "a converged check within the budget of twenty-four; the reads:\n%s", strings.Join(gets, "\n"))
	require.Positive(t, check.Requests.CircleCI, "the circleci step reads the project, its definitions and their triggers")

	// The override costs nothing: the caller hands it in with the entry
	// (--team-file reads it from the checkout), and the converged check
	// of a repository whose CODEOWNERS is its override keeps the budget.
	h.repo().files["CODEOWNERS"] = testCodeownersOverride
	h.codeownersOverride = []byte(testCodeownersOverride)
	overridden := h.run(ModeCheck, false)
	require.True(t, overridden.Converged, "%+v", overridden.Steps)
	require.Equal(t, "matches the override repositories/override/sample-service/CODEOWNERS of giantswarm/github", overridden.Step(StepCodeowners).Summary)
	require.Equal(t, check.Requests.GitHub, overridden.Requests.GitHub, "the override adds no request")
	data, err = json.Marshal(check)
	require.NoError(t, err)
	require.Contains(t, string(data), fmt.Sprintf(`"requests":{"github":%d,"circleci":%d}`, check.Requests.GitHub, check.Requests.CircleCI))
	require.Empty(t, Refused(Request{Team: team, Entry: h.entry}, time.Now()).Requests, "a refused entry costs nothing")
}

func TestRunArchivedDeclarationRunsLifecycleOnly(t *testing.T) {
	h := newHarness(t, archivedEntryYAML)
	h.gh.addRepo(owner, name)
	res := h.run(ModeCheck, false)
	for _, sr := range res.Steps {
		switch sr.Step {
		case StepCreate:
			require.Equal(t, VerdictOK, sr.Verdict)
		case StepLifecycle:
			require.Equal(t, VerdictDrift, sr.Verdict)
		default:
			require.Equal(t, VerdictSkipped, sr.Verdict, "%s", sr.Step)
			require.Equal(t, "lifecycle: archived", sr.Summary)
		}
	}
}

func TestRunDeletedDeclarationRunsLifecycleOnly(t *testing.T) {
	h := newHarness(t, deletedEntryYAML)
	h.gh.addRepo(owner, name)
	res := h.run(ModeCheck, false)
	require.False(t, res.Converged)
	for _, sr := range res.Steps {
		switch sr.Step {
		case StepCreate:
			require.Equal(t, VerdictOK, sr.Verdict)
		case StepLifecycle:
			require.Equal(t, VerdictDrift, sr.Verdict)
			require.Equal(t, []string{"delete on GitHub"}, sr.Changes, "not followed on CircleCI: nothing to unfollow")
		default:
			require.Equal(t, VerdictSkipped, sr.Verdict, "%s", sr.Step)
			require.Equal(t, "lifecycle: deleted", sr.Summary)
		}
	}
}

// TestRunForkLineFollowsItsDeclaredBranch: a fork line is on the branch its
// entry declares, and that branch is the one the engine keeps and protects.
// The dry run plans no rename, protection on the declared branch, and skips
// the scaffold and CODEOWNERS steps; the repair protects the declared branch
// and leaves the tree alone.
func TestRunForkLineFollowsItsDeclaredBranch(t *testing.T) {
	// Without the App id the protection is classic and names the branch.
	t.Run("classic protection names the declared branch", func(t *testing.T) {
		h := newHarness(t, forkEntryYAML)
		h.runner.DevctlAppID = 0
		r := h.gh.addRepo(owner, name).forkLine()
		delete(r.files, "CODEOWNERS")

		check := h.run(ModeCheck, false)
		for _, sr := range check.Steps {
			require.NotEqual(t, VerdictFailed, sr.Verdict, "%s: %s", sr.Step, sr.Summary)
			switch sr.Step {
			case StepScaffold, StepCodeowners:
				require.Equal(t, VerdictSkipped, sr.Verdict, "%s: %+v", sr.Step, sr)
				require.Equal(t, "flavour fork", sr.Summary, "%s", sr.Step)
			case StepSettings:
				require.Equal(t, VerdictOK, sr.Verdict, "no rename, and the rebase merges stay: %+v", sr)
			case StepProtection:
				require.Equal(t, VerdictDrift, sr.Verdict, "%+v", sr)
				require.Contains(t, sr.Changes, "protect giantswarm", "%+v", sr.Changes)
			}
		}
		require.Empty(t, h.mutations(), "a check must not write")

		repair := h.run(ModeRepair, false)
		require.Equal(t, VerdictRepaired, repair.Step(StepProtection).Verdict, "%+v", repair.Step(StepProtection))
		require.Equal(t, "giantswarm", r.protected, "the declared branch is the protected one")
		require.Equal(t, "giantswarm", r.defaultBranch)
		require.Empty(t, r.prs, "no CODEOWNERS pull request")
		require.NotContains(t, r.files, "CODEOWNERS", "the tree is upstream's")
	})

	// With the App id the ruleset follows the default branch, which the
	// settings step keeps on the declared one.
	t.Run("the ruleset follows the declared branch", func(t *testing.T) {
		h := newHarness(t, forkEntryYAML)
		r := h.gh.addRepo(owner, name).forkLine()
		delete(r.files, "CODEOWNERS")

		check := h.run(ModeCheck, false)
		sr := check.Step(StepProtection)
		require.Equal(t, VerdictDrift, sr.Verdict, "%+v", sr)
		require.Contains(t, strings.Join(sr.Changes, "; "), `create ruleset "devctl: default branch"`)
		require.Empty(t, h.mutations(), "a check must not write")

		repair := h.run(ModeRepair, false)
		require.Equal(t, VerdictRepaired, repair.Step(StepProtection).Verdict, "%+v", repair.Step(StepProtection))
		require.Equal(t, []string{"~DEFAULT_BRANCH"}, r.ruleset(RulesetName).Conditions.RefName.Include, "the ruleset follows the default branch")
		require.Equal(t, "giantswarm", r.defaultBranch, "which stays the declared one")
		require.Nil(t, r.protection, "no classic protection beside it")
	})
}

// TestRunCustomerFlavourKeepsTheCustomersFlow: a customer repository has
// branch protection of the customer's own, a default branch that is theirs
// and no CircleCI pipeline of ours. The dry run plans settings changes at
// most — no rename, no protection, no follow or deploy key, no CODEOWNERS
// pull request — and the repair leaves all of that alone too.
func TestRunCustomerFlavourKeepsTheCustomersFlow(t *testing.T) {
	h := newHarness(t, customerEntryYAML)
	r := h.gh.addRepo(owner, name)
	r.defaultBranch = "master"
	r.hasWiki = true
	r.protection = &fakeProtection{reviews: 2, enforceAdmins: false, strict: true, checks: []string{"customer/build"}}
	delete(r.files, "CODEOWNERS")
	theirs := *r.protection

	check := h.run(ModeCheck, false)
	for _, sr := range check.Steps {
		require.NotEqual(t, VerdictFailed, sr.Verdict, "%s: %s", sr.Step, sr.Summary)
		switch sr.Step {
		case StepProtection, StepCircleCI, StepCodeowners, StepRelease:
			require.Equal(t, VerdictSkipped, sr.Verdict, "%s: %+v", sr.Step, sr)
			require.Equal(t, "flavour customer", sr.Summary, "%s", sr.Step)
			require.Empty(t, sr.Changes, "%s plans nothing: no protection, no follow, no deploy key, no CODEOWNERS pull request", sr.Step)
		case StepSettings:
			require.Equal(t, VerdictDrift, sr.Verdict, "%+v", sr)
			require.Equal(t, []string{"settings: has_wiki true → false"}, sr.Changes, "the settings baseline applies, the branch is not renamed")
		}
	}
	require.Empty(t, h.mutations(), "a check must not write")

	repair := h.run(ModeRepair, false)
	require.Equal(t, VerdictRepaired, repair.Step(StepSettings).Verdict, "%+v", repair.Step(StepSettings))
	require.False(t, r.hasWiki)
	require.Equal(t, "master", r.defaultBranch, "the default branch is the customer's")
	require.Equal(t, theirs, *r.protection, "protection is the customer's")
	require.Empty(t, h.cc.projects, "not followed on CircleCI, no deploy key")
	require.NotContains(t, r.files, "CODEOWNERS")
	require.Empty(t, r.prs, "no CODEOWNERS pull request")
}

// A declared deletion whose repository is gone is the record: converged, no
// finding, nothing to do — on the schedule as on a push.
func TestRunDeletedDeclarationOfAGoneRepositoryIsTheRecord(t *testing.T) {
	h := newHarness(t, deletedEntryYAML)
	for _, added := range []bool{false, true} {
		res := h.run(ModeRepair, added)
		require.True(t, res.Converged, "added=%v", added)
		require.Empty(t, res.Findings(), "added=%v", added)
		require.Equal(t, "deleted, as declared", res.Step(StepCreate).Summary)
		for _, sr := range res.Steps {
			if sr.Step != StepCreate {
				require.Equal(t, VerdictSkipped, sr.Verdict, "%s", sr.Step)
				require.Equal(t, "deleted, as declared", sr.Summary)
			}
		}
		require.Empty(t, h.mutations(), "added=%v: nothing is created or written", added)
	}
}

func TestRunRefusesWhatCannotRun(t *testing.T) {
	h := newHarness(t, entryYAML)
	_, err := h.runner.Run(context.Background(), Request{Team: team, Entry: reposetup.Entry{Name: name}})
	require.True(t, IsInvalidConfig(err), "an unaccepted entry: %v", err)
	_, err = h.runner.Run(context.Background(), Request{Team: team, Entry: h.entry, Steps: []Step{"paint"}})
	require.True(t, IsInvalidConfig(err), "an unknown step: %v", err)
	_, err = (&Runner{}).Run(context.Background(), Request{Team: team, Entry: h.entry})
	require.True(t, IsInvalidConfig(err), "no GitHub client: %v", err)
}

// TestRefused: an entry the validator refused is a result the callers parse,
// not an exit without one.
func TestRefused(t *testing.T) {
	now := time.Date(2026, 9, 16, 22, 0, 0, 0, time.UTC)
	entry := reposetup.Entry{Name: name, Problems: []reposetup.Problem{
		{Field: "gen.ci.generate", Message: "no CircleCI job for language generic without the app flavour or gen.ci.image.dockerfile; set it to false"},
		{Field: "gen.language", Message: "the Node template is not available yet"},
	}}
	res := Refused(Request{Team: team, Entry: entry, Added: true}, now)

	require.Equal(t, owner+"/"+name, res.Repository)
	require.Equal(t, res.Repository, res.Declared)
	require.Equal(t, ModeCheck, res.Mode, "empty mode defaults as Run does")
	require.True(t, res.Added)
	require.False(t, res.Converged, "nothing was checked: a refused entry is not set up as declared")
	require.True(t, res.Refused(), "the entry step tells the refusal from drift")
	require.Empty(t, res.Failed(), "exit 0")
	require.Len(t, res.Steps, 1)
	sr := res.Steps[0]
	require.Equal(t, StepEntry, sr.Step)
	require.Equal(t, VerdictReported, sr.Verdict)
	require.Equal(t, []FindingKind{FindingGenCircleCIRefused, FindingEntryRefused}, kinds(sr.Findings))
	require.Equal(t, "gen.ci.generate: no CircleCI job for language generic without the app flavour or gen.ci.image.dockerfile; set it to false", sr.Findings[0].Message)
	require.Equal(t, `edit gen.language of the entry "sample-service" in repositories/team-bumblebee.yaml: the Node template is not available yet`, sr.Findings[1].Fix)

	data, err := json.Marshal(res)
	require.NoError(t, err)
	require.Contains(t, string(data), `"step":"entry","verdict":"reported"`)
	require.Contains(t, string(data), `"kind":"entry-refused"`)
	require.Contains(t, string(data), `"converged":false`)

	var table strings.Builder
	require.NoError(t, res.WriteTable(&table))
	require.True(t, strings.HasPrefix(table.String(), owner+"/"+name+" (check): not converged, entry refused in 0s\n"), table.String())

	ran := &Result{Steps: []StepResult{{Step: StepSettings, Verdict: VerdictDrift}}}
	require.False(t, ran.Refused(), "drift is not a refusal")
}

func TestRequiredChecks(t *testing.T) {
	b := DefaultBaseline()
	b.RequiredChecks = []string{"PR Gatekeeper"}
	gates := []string{ctxGoBuild, "ci/circleci: build-chart"}

	got, err := requiredChecks(b, nil,
		[]string{ctxGoBuild, ctxSetup, ctxDepGraph, "execute-smoke-test", ctxGhost},
		[]string{ctxGoBuild, ctxSetup, ctxDepGraph, "execute-smoke-test", ctxSemantic, "ci/circleci: build-chart"}, true,
		gates, true)
	require.NoError(t, err)
	require.Equal(t, []string{"PR Gatekeeper", ctxGoBuild, "execute-smoke-test", ctxSemantic, "ci/circleci: build-chart"}, got,
		"unconditional first; the reported hand-added gate stays; the stale CircleCI job, the ignored context and the ghost go; the candidates that reported join")

	got, err = requiredChecks(b, nil, []string{ctxGoBuild, ctxGhost}, nil, false, nil, false)
	require.NoError(t, err)
	require.Equal(t, []string{"PR Gatekeeper", ctxGoBuild, ctxGhost}, got, "unknown reports and pipeline: nothing removed, nothing added")

	got, err = requiredChecks(b, []string{ctxValidate, ctxDepGraph, ctxSetup}, []string{ctxGoBuild, ctxSetup}, []string{ctxGoBuild}, true, gates, true)
	require.NoError(t, err)
	require.Equal(t, []string{"PR Gatekeeper", ctxValidate, ctxDepGraph, ctxSetup, ctxGoBuild}, got,
		"the declared contexts follow the baseline's whatever reported: the unreported one, the ignored one and the stale CircleCI job alike")
}

// TestScaffoldGoServiceWithChart is the engine test of the Go service with
// the app flavour: the real renderer, on the test templates, scaffolds the
// Go template with the chart template's chart, so the scaffold step finds
// the chart at helm/<name> with the team annotation and the values schema
// and reports nothing app-build-suite would fail on -- only the default
// icon.
func TestScaffoldGoServiceWithChart(t *testing.T) {
	h := newHarness(t, entryYAML)
	h.runner.Renderer = reposetup.Renderer{Templates: reposetup.DirTemplates{Root: filepath.Join("..", "testdata", "templates")}}
	r := h.gh.addRepo(owner, name)
	r.empty, r.files = true, map[string]string{}

	res := h.run(ModeRepair, false, StepScaffold)
	sr := res.Step(StepScaffold)
	require.NotNil(t, sr)
	require.Equal(t, []FindingKind{FindingDefaultIcon}, kinds(sr.Findings), "the chart is there: no prerequisite finding")

	files := h.repo().files
	require.Contains(t, files, "main.go", "the Go template's files")
	require.Contains(t, files["helm/sample-service/Chart.yaml"], `io.giantswarm.application.team: "bumblebee"`)
	require.Contains(t, files, "helm/sample-service/values.yaml")
	require.Contains(t, files, "helm/sample-service/values.schema.json")
	require.Contains(t, files, "helm/sample-service/templates/_helpers.tpl")
	require.Contains(t, files[".abs/main.yaml"], "chart-dir: ./helm/sample-service")
	require.Equal(t, scaffoldSubject, h.repo().headSubject("main"))
}

// TestScaffoldWithoutGeneratedCIConfiguresRenovate: a repository created
// without generated CI -- the minimal scaffold of a generic/generic entry
// with gen.ci.generate off -- carries renovate.json5 from its scaffold, so
// the reconciler's first run over it has no Renovate finding: the renovate
// step is ok, Renovate's first run not due yet on a repository created
// minutes ago.
func TestScaffoldWithoutGeneratedCIConfiguresRenovate(t *testing.T) {
	h := newHarness(t, configurationEntryYAML)
	h.runner.Renderer = reposetup.Renderer{Templates: reposetup.DirTemplates{Root: filepath.Join("..", "testdata", "templates")}}
	r := h.gh.addRepo(owner, name)
	r.empty, r.files, r.createdAt = true, map[string]string{}, time.Now().Add(-time.Minute)
	h.gh.installation.status = 403

	scaffold := h.run(ModeRepair, false, StepScaffold)
	require.Equal(t, VerdictRepaired, scaffold.Step(StepScaffold).Verdict, "%+v", scaffold.Step(StepScaffold))
	config := h.repo().files["renovate.json5"]
	require.Contains(t, config, "github>giantswarm/renovate-presets:default.json5", "the scaffold's Renovate configuration")
	require.NotContains(t, config, "giantswarm/architect", "no generated CI: no architect orb for Renovate to leave alone")
	require.NotContains(t, h.repo().files, ".circleci/config.yml")

	first := h.run(ModeCheck, false, StepRenovate)
	sr := first.Step(StepRenovate)
	require.Equal(t, VerdictOK, sr.Verdict, "%+v", sr)
	require.Empty(t, sr.Findings)
	require.Equal(t, "renovate.json5; no Renovate run yet and none due: the repository is younger than a day, Renovate's first run follows", sr.Summary)
}

// TestStepConverges: the verdict matrix of the converged mark. A step
// converges unless it drifted, failed, or carries a finding that is not
// advisory; the default icon alone is advisory.
func TestStepConverges(t *testing.T) {
	advisory := newFinding(FindingDefaultIcon, "default icon", "replace it")
	toFix := newFinding(FindingABSPrerequisite, "no schema", "add it")
	require.True(t, advisory.Advisory)
	require.False(t, toFix.Advisory)
	cases := []struct {
		name string
		step StepResult
		want bool
	}{
		{name: "ok", step: StepResult{Verdict: VerdictOK}, want: true},
		{name: "skipped", step: StepResult{Verdict: VerdictSkipped}, want: true},
		{name: "repaired", step: StepResult{Verdict: VerdictRepaired}, want: true},
		{name: "drift", step: StepResult{Verdict: VerdictDrift}, want: false},
		{name: "failed", step: StepResult{Verdict: VerdictFailed}, want: false},
		{name: "reported, advisory only", step: StepResult{Verdict: VerdictReported, Findings: []Finding{advisory}}, want: true},
		{name: "reported, a finding to fix", step: StepResult{Verdict: VerdictReported, Findings: []Finding{toFix}}, want: false},
		{name: "reported, advisory next to a finding to fix", step: StepResult{Verdict: VerdictReported, Findings: []Finding{advisory, toFix}}, want: false},
		{name: "repaired with the default icon", step: StepResult{Verdict: VerdictRepaired, Findings: []Finding{advisory}}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.step.Converges())
		})
	}
}

// TestRunProtectionTeamRefusedByGitHub: GitHub's own refusal of the team as
// bypass actor (422), beyond what its privacy shows, is caught: the ruleset
// is written with the App alone and the team is reported with the fix.
func TestRunProtectionTeamRefusedByGitHub(t *testing.T) {
	h := newHarness(t, entryYAML)
	h.gh.teamBypassRefused = true
	r := h.gh.addRepo(owner, name)
	r.statuses = []string{ctxGoBuild}
	r.addRuleset(RulesetName, []*github.RuleStatusCheck{statusCheck(ctxGoBuild)}, appBypass(testAppID))

	res := h.run(ModeRepair, false, StepProtection)
	sr := res.Step(StepProtection)
	require.Equal(t, VerdictRepaired, sr.Verdict, "%+v", sr)
	require.Equal(t, []string{"bypass actors: App 424242 on pull requests, repository admins on pull requests, team team-bumblebee on pull requests"}, sr.Changes)
	require.Equal(t, []FindingKind{FindingTeamBypassRefused}, kinds(sr.Findings))
	require.Contains(t, sr.Findings[0].Message, "refused team "+team+" (privacy closed)")
	require.Contains(t, sr.Findings[0].Message, "written with the App and the repository admins")
	rs := h.repo().ruleset(RulesetName)
	require.Equal(t, []*github.BypassActor{appBypass(testAppID), adminBypass()}, rs.BypassActors, "the App and the admins")
	require.Equal(t, []string{ctxGoBuild}, checkContexts(rs), "nothing else changes")
	require.Len(t, h.mutations(), 2, "the refused write and the one without the team")
}
