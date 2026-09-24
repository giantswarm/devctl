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

	"github.com/google/go-github/v92/github"
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
	// declaredRulesetEntryYAML declares a ruleset of the repository's own the
	// team keeps beside the engine's.
	declaredRulesetEntryYAML = entryYAML + "  rulesets: [\"protect-giantswarm\"]\n"
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

	// testAppID is the devctl App's id the harness configures.
	testAppID int64 = 424242
)

// harness wires a Runner to the two fakes with a validated entry.
type harness struct {
	t        *testing.T
	gh       *fakeGitHub
	cc       *fakeCircleCI
	runner   *Runner
	baseline Baseline
	entry    reposetup.Entry
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
	cc.onFollow = gh.installHook // a follow by an admin with the hook scope
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
	res, err := h.runner.Run(context.Background(), Request{Team: team, Entry: h.entry, Added: added, Mode: mode, Steps: steps})
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
	doc := "---\napiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n    name: " + n + "\n"
	if len(charts) == 0 {
		return doc
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
				require.True(t, res.Converged)
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
			name: "circleci: follow, setup workflows, checkout key", step: StepCircleCI,
			seed:      func(h *harness) { h.gh.addRepo(owner, name) },
			wantCheck: VerdictDrift, wantChange: "follow giantswarm/sample-service; enable setup workflows; create a deploy key",
			verify: func(t *testing.T, h *harness, _ *Result) {
				p := h.cc.projects[owner+"/"+name]
				require.NotNil(t, p)
				require.True(t, p.setupWorkflows)
				require.Len(t, p.keys, 1)
			},
		},
		{
			name: "circleci: the follow identity without admin is granted it for the follow and revoked after", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.gh.permission = "write" // architectbot holds push through the bots team
			},
			wantCheck:  VerdictDrift,
			wantChange: "grant architectbot admin for the CircleCI follow, revoked after it; follow giantswarm/sample-service; enable setup workflows; create a deploy key",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Contains(t, h.cc.projects, owner+"/"+name)
				require.Empty(t, h.repo().collaborators, "the grant is revoked once the project is followed")
				mutations := strings.Join(h.gh.mutations, "\n")
				require.Contains(t, mutations, "PUT /repos/giantswarm/sample-service/collaborators/architectbot")
				require.Contains(t, mutations, "DELETE /repos/giantswarm/sample-service/collaborators/architectbot")
			},
		},
		{
			name: "circleci: a followed project with its webhook reads ok", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.cc.follow(owner, name)
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "followed, setup workflows on, checkout key present, webhook present", res.Step(StepCircleCI).Summary)
			},
		},
		{
			// giantswarm/mcp-toolkit: followed, with setup workflows and a
			// deploy key, and GET /repos/{owner}/{repo}/hooks answered []:
			// no push and no tag reached CircleCI, and the step read ok. An
			// inactive CircleCI hook or another system's hook is no webhook
			// either.
			name: "circleci: a followed project without CircleCI's webhook is reported", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				h.cc.onFollow = nil
				h.cc.follow(owner, name)
				inactive := circleCIHook()
				inactive.Active = new(false)
				r.hooks = []*github.Hook{inactive, {ID: new(int64(7)), Name: new("web"), Active: new(true), Events: []string{"push"}, Config: &github.HookConfig{URL: new("https://github-pr-webhook.ci.giantswarm.io")}}}
			},
			wantCheck: VerdictReported, wantFinding: FindingCircleCIWebhookMissing, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				sr := res.Step(StepCircleCI)
				require.Equal(t, "followed, setup workflows on, checkout key present, webhook missing", sr.Summary)
				require.Len(t, sr.Findings, 1)
				require.False(t, sr.Findings[0].Advisory, "a deaf project is not set up")
				require.Contains(t, sr.Findings[0].Fix, "POST /api/v1.1/project/github/giantswarm/sample-service/follow")
				require.False(t, sr.Converges())
			},
		},
		{
			// The reconciler's follow as architectbot under a temporary admin
			// grant: CircleCI followed the project and installed no hook,
			// because that account's CircleCI grant lacks the hook scope. The
			// follow is repaired, the missing hook reported in the same run.
			name: "circleci: a follow whose grant lacks the hook scope leaves the webhook missing", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.cc.onFollow = nil
			},
			wantCheck: VerdictDrift, wantChange: "follow giantswarm/sample-service; enable setup workflows; create a deploy key",
			wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Contains(t, h.cc.projects, owner+"/"+name)
				require.Equal(t, []FindingKind{FindingCircleCIWebhookMissing}, kinds(res.Step(StepCircleCI).Findings))
			},
		},
		{
			name: "circleci: webhooks the identity cannot read are unchecked", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				h.cc.follow(owner, name)
				r.hooksStatus = 404
			},
			wantCheck: VerdictReported, wantFinding: FindingUnchecked, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Equal(t, "followed, setup workflows on, checkout key present, webhook not readable by this identity", res.Step(StepCircleCI).Summary)
			},
		},
		{
			name: "circleci: a followed project without setup workflows and key (template-app's defects)", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.hooks = []*github.Hook{circleCIHook()}
				h.cc.projects[owner+"/"+name] = &fakeProject{}
			},
			wantCheck: VerdictDrift, wantChange: "enable setup workflows; create a deploy key",
		},
		{
			name: "circleci: a renamed repository is followed under its new slug", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name+"-v2")
				h.gh.redirects[owner+"/"+name] = owner + "/" + name + "-v2"
			},
			wantCheck: VerdictDrift, wantChange: "follow giantswarm/sample-service-v2 under its new slug (declared as sample-service)",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Contains(t, h.cc.projects, owner+"/"+name+"-v2")
				require.NotContains(t, h.cc.projects, owner+"/"+name)
			},
		},
		{
			// A configuration repository, or one released by GitHub Actions,
			// has no .circleci/config.yml and declares no generated pipeline:
			// CircleCI has nothing to build, so the repository is neither
			// followed nor given a key, and its release is not held against
			// a pipeline. A project followed by hand is left as it is.
			name: "circleci: a repository without a pipeline is not followed, its release not verified", step: StepCircleCI,
			entry: configurationEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				delete(r.files, ".circleci/config.yml")
				delete(r.files, ".circleci/workflows.yml")
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.NotContains(t, h.cc.projects, owner+"/"+name)
				config := "/repos/" + owner + "/" + name + "/contents/" + circleCIConfig
				before := h.gh.reads(config)
				res := h.run(ModeCheck, false, StepCircleCI, StepRelease)
				for _, step := range []Step{StepCircleCI, StepRelease} {
					require.Equal(t, VerdictSkipped, res.Step(step).Verdict, "%s: %+v", step, res.Step(step))
					require.Equal(t, "no CircleCI pipeline", res.Step(step).Summary)
				}
				require.Equal(t, before+1, h.gh.reads(config), "one read of the branch serves both steps")

				h.cc.projects[owner+"/"+name] = &fakeProject{} // followed by hand, no setup workflows, no key
				res = h.run(ModeRepair, false, StepCircleCI, StepRelease)
				require.Equal(t, VerdictSkipped, res.Step(StepCircleCI).Verdict, "%+v", res.Step(StepCircleCI))
				require.Equal(t, VerdictSkipped, res.Step(StepRelease).Verdict, "%+v", res.Step(StepRelease))
				require.Empty(t, h.mutations(), "a followed project of a repository without a pipeline is left as it is")
			},
		},
		{
			// A template repository's .circleci/config.yml is content for the
			// repositories created from it (gen.ci.templateContent): the branch
			// carries the file, yet CircleCI has nothing to build, so the
			// repository is neither followed nor given a key, its release is
			// not held against a pipeline, and the branch is not read.
			name: "circleci: template content is no pipeline, whatever the branch carries", step: StepCircleCI, existing: true,
			entry: templateContentEntryYAML,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.NotContains(t, h.cc.projects, owner+"/"+name)
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
			// yet: the entry declares it (gen.ci.generate: true, the default
			// the validator renders for a service with a CI job), and the
			// follow neither waits for the scaffold's commit nor reads the
			// branch.
			name: "circleci: a generated pipeline is followed before its config is on the branch", step: StepCircleCI,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				delete(r.files, ".circleci/config.yml")
				delete(r.files, ".circleci/workflows.yml")
			},
			wantCheck: VerdictDrift, wantChange: "follow giantswarm/sample-service; enable setup workflows; create a deploy key",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Contains(t, h.cc.projects, owner+"/"+name)
				require.Zero(t, h.gh.reads("/repos/"+owner+"/"+name+"/contents/"+circleCIConfig), "a declared pipeline is not looked up")
			},
		},
		{
			// The reconciler validates a declared repository's entry as an
			// existing one, rendered as declared: an entry with gen and no
			// gen.ci does not get the creation default gen.ci.generate: true
			// — the schema's word is that it keeps the repository's own
			// CircleCI configuration — so the branch decides. A repository
			// released by GitHub Actions has no .circleci/config.yml there:
			// it is neither followed nor given a key, and its release is not
			// held against a tag build CircleCI never ran.
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
				require.NotContains(t, h.cc.projects, owner+"/"+name)
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
			// branch says there is a pipeline, and the step keeps it followed.
			name: "circleci: an existing entry without gen.ci follows the pipeline the branch carries", step: StepCircleCI, existing: true,
			seed:      func(h *harness) { h.gh.addRepo(owner, name) },
			wantCheck: VerdictDrift, wantChange: "follow giantswarm/sample-service; enable setup workflows; create a deploy key",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Contains(t, h.cc.projects, owner+"/"+name)
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
			name: "lifecycle: archived archives on GitHub and unfollows on CircleCI", step: StepLifecycle, entry: archivedEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.cc.follow(owner, name)
			},
			wantCheck: VerdictDrift, wantChange: "archive on GitHub; unfollow on CircleCI and stop building",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.True(t, h.repo().archived)
				p := h.cc.projects[owner+"/"+name]
				require.NotNil(t, p, "the project stays on CircleCI, as it does live")
				require.False(t, p.following, "the token's user unfollowed")
				require.False(t, p.building, "stopped building")
			},
		},
		{
			name: "lifecycle: an archived repository the token's user does not follow is left alone", step: StepLifecycle, entry: archivedEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name).archived = true
				h.cc.follow(owner, name).following = false // set up by someone else, or unfollowed by an earlier run
			},
			wantCheck: VerdictOK,
		},
		{
			name: "lifecycle: deleted unfollows on CircleCI and deletes on GitHub; the next run finds the record", step: StepLifecycle, entry: deletedEntryYAML,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.cc.follow(owner, name)
			},
			wantCheck: VerdictDrift, wantChange: "unfollow on CircleCI and stop building; delete on GitHub",
			// The second repair finds no repository: the create step reads the
			// declaration as the record and the lifecycle step is skipped on it.
			wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, repair *Result) {
				_, exists := h.gh.repos[owner+"/"+name]
				require.False(t, exists, "deleted on GitHub")
				p := h.cc.projects[owner+"/"+name]
				require.NotNil(t, p, "the project stays on CircleCI, as it does live")
				require.False(t, p.following, "the token's user unfollowed")
				require.False(t, p.building, "stopped building")
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
			// The reconciler never rebuilds a tag: a trigger would publish a
			// chart or an image nobody asked for. The missed build is a
			// finding in every mode and no request is written.
			name: "release: a tag without a pipeline is a missed build, reported and never triggered", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				h.cc.follow(owner, name)
			},
			wantCheck: VerdictReported, wantFinding: FindingMissedTagBuild, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, res *Result) {
				f := res.Findings()[0]
				require.Equal(t, "release v0.1.0 of "+owner+"/"+name+" has no pipeline: nothing was built or published for the tag", f.Message)
				require.Equal(t, "cut the next tag, or trigger the tag's pipeline by hand", f.Fix)
				require.Empty(t, h.cc.mutations, "no pipeline is triggered")
				require.Empty(t, h.cc.projects[owner+"/"+name].pipelines)
			},
		},
		{
			name: "release: a red tag pipeline is reported, the fix a rerun from failed", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				h.cc.addPipeline(h.cc.follow(owner, name), "v0.1.0", "failed")
			},
			wantCheck: VerdictReported, wantFinding: FindingRedRelease, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				f := res.Findings()[0]
				require.Contains(t, f.Message, "push-to-registries-release")
				require.Contains(t, f.Fix, "rerun the failed workflow from failed")
				require.Contains(t, f.Fix, "devctl release wait "+owner+"/"+name+" v0.1.0")
			},
		},
		{
			// A rerun from failed is a second workflow of the same name in
			// the same pipeline; the run it replaced keeps its failed status.
			// The newest run decides, so the revived tag reads built.
			name: "release: a red tag pipeline rerun green reads built", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				p := h.cc.follow(owner, name)
				h.cc.addPipeline(p, "v0.1.0", "failed")
				h.cc.rerun(p, "success")
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Contains(t, res.Step(StepRelease).Summary, "release v0.1.0 built")
				require.Empty(t, res.Findings())
			},
		},
		{
			name: "release: a green tag pipeline", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				h.cc.addPipeline(h.cc.follow(owner, name), "v0.1.0", "success")
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Contains(t, res.Step(StepRelease).Summary, "release v0.1.0 built")
			},
		},
		{
			// A repository created pull-request-last: the tag exists before
			// the project is followed, and the follow builds the default
			// branch first. The newer pipeline is no evidence for the tag.
			name: "release: a newer pipeline of another ref does not hide the missed tag build", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				h.cc.seedPipeline(h.cc.follow(owner, name), circleciclient.PipelineVCS{Branch: "main"}, time.Now(), "success")
			},
			wantCheck: VerdictReported, wantFinding: FindingMissedTagBuild, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Empty(t, h.cc.mutations, "no pipeline is triggered")
				require.Len(t, h.cc.projects[owner+"/"+name].pipelines, 1, "the default branch's pipeline alone")
			},
		},
		{
			// The tag's pipeline is behind a full first page of newer
			// pipelines: the step pages on and finds it.
			name: "release: the tag pipeline on a later page is verified", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				h.cc.pageSize = 1
				p := h.cc.follow(owner, name)
				h.cc.seedPipeline(p, circleciclient.PipelineVCS{Tag: "v0.1.0"}, time.Now().Add(-time.Minute), "success")
				h.cc.seedPipeline(p, circleciclient.PipelineVCS{Branch: "main"}, time.Now(), "success")
			},
			wantCheck: VerdictOK,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Contains(t, res.Step(StepRelease).Summary, "release v0.1.0 built")
				require.Len(t, h.cc.projects[owner+"/"+name].pipelines, 2)
				require.Contains(t, h.cc.pages, "1", "the second page was read")
			},
		},
		{
			// Paging stops at the first pipeline older than the release: the
			// tag's pipeline would have been listed before it. The pages
			// behind it are never read; the missed build is reported.
			name: "release: paging stops behind the release and reports the missed tag build", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				h.cc.pageSize = 1
				p := h.cc.follow(owner, name)
				h.cc.seedPipeline(p, circleciclient.PipelineVCS{Branch: "main"}, time.Now().Add(-3*time.Hour), "success")
				h.cc.seedPipeline(p, circleciclient.PipelineVCS{Branch: "main"}, time.Now().Add(-2*time.Hour), "success")
				h.cc.seedPipeline(p, circleciclient.PipelineVCS{Branch: "main"}, time.Now(), "success")
			},
			wantCheck: VerdictReported, wantFinding: FindingMissedTagBuild, wantAfter: VerdictReported,
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Empty(t, h.cc.mutations, "no pipeline is triggered")
				require.Len(t, h.cc.projects[owner+"/"+name].pipelines, 3)
				require.Contains(t, h.cc.pages, "1", "the page with the older pipeline was read")
				require.NotContains(t, h.cc.pages, "2", "the pages behind the release were not read")
			},
		},
		{
			// A repository tagging per component (base/v0.1.0) releases
			// outside the flow the step verifies: auto-release cuts vX.Y.Z
			// and the generated pipeline builds /^v.*/. Its latest release
			// is not held against CircleCI: the step is skipped naming the
			// tag, no pipeline list is read and nothing is reported.
			name: "release: a release whose tag is not vX.Y.Z is not verified", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "base/v0.1.0", time.Now().Add(-time.Hour)
				h.cc.follow(owner, name)
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, "release base/v0.1.0: not a vX.Y.Z tag, not verified", res.Step(StepRelease).Summary)
				require.Empty(t, res.Findings())
				require.Empty(t, h.cc.pages, "no pipeline list is read")
				require.Empty(t, h.cc.mutations, "no pipeline is triggered")
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

// TestRunFullRepositorySetUp runs every step over a repository that only
// exists as an added entry: the first repair converges the whole set-up
// (bar the pull request and the findings for a person), the second changes
// nothing.
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
	require.Equal(t, VerdictRepaired, first.Step(StepCircleCI).Verdict)
	require.Equal(t, VerdictRepaired, first.Step(StepCatalog).Verdict)
	require.Equal(t, VerdictOK, first.Step(StepCodeowners).Verdict, "the scaffold's CODEOWNERS names the team")
	require.Equal(t, VerdictOK, first.Step(StepRelease).Verdict, "no release yet")
	require.Empty(t, checkContexts(h.repo().ruleset(RulesetName)), "nothing has reported on a fresh repository, so nothing is required")
	require.Nil(t, h.repo().protection, "the protection is the ruleset, no classic protection")
	require.True(t, first.Converged)

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
	// most twenty-one GitHub requests (the webhooks read the circleci step
	// added is the twenty-first), and the count is the fake's own.
	before := len(h.gets())
	check := h.run(ModeCheck, false)
	require.True(t, check.Converged, "%+v", check.Steps)
	gets := h.gets()[before:]
	require.Equal(t, len(gets), check.Requests.GitHub, "the counter and the fake agree")
	require.LessOrEqual(t, check.Requests.GitHub, 21, "a converged check within the budget of twenty-one; the reads:\n%s", strings.Join(gets, "\n"))
	require.Positive(t, check.Requests.CircleCI, "the circleci and release steps read CircleCI")
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
