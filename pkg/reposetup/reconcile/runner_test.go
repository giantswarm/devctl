package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

	ctxGoBuild  = "ci/circleci: go-build"
	ctxSetup    = "ci/circleci: setup"
	ctxDepGraph = "update-go_modules-graph"
	ctxSemantic = "semantic-pull-request / Validate PR title"
	ctxRelease  = "create-release / Gather facts"
	ctxGhost    = "CircleCI Pipeline"
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

func newHarness(t *testing.T, yaml string) *harness {
	t.Helper()
	ctx := context.Background()
	gh, cc := newFakeGitHub(), newFakeCircleCI()
	t.Cleanup(gh.srv.Close)
	t.Cleanup(cc.srv.Close)

	ghClient, err := github.NewClient(github.WithEnterpriseURLs(gh.srv.URL, gh.srv.URL))
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	checks, err := githubclient.New(githubclient.Config{Logger: logger, AccessToken: "token", BaseURL: gh.srv.URL})
	require.NoError(t, err)

	ccClient, err := circleciclient.New(circleciclient.Config{Token: "token", BaseURL: cc.srv.URL})
	require.NoError(t, err)

	schema, err := reposetup.EmbeddedSchema()
	require.NoError(t, err)
	tf, err := reposetup.ParseTeamFile(team, strings.NewReader(yaml))
	require.NoError(t, err)
	validated, err := reposetup.Validator{Schema: schema}.Validate(ctx, reposetup.Request{TeamFile: tf})
	require.NoError(t, err)
	require.True(t, validated.Entries[0].Accepted, "%v", validated.Entries[0].Problems)

	h := &harness{t: t, gh: gh, cc: cc, baseline: DefaultBaseline(), entry: validated.Entries[0]}
	h.runner = &Runner{GitHub: ghClient, Checks: checks, CircleCI: ccClient, Renderer: fakeRenderer{}, Baseline: &h.baseline}
	return h
}

func (h *harness) run(mode Mode, added bool, steps ...Step) *Result {
	h.t.Helper()
	res, err := h.runner.Run(context.Background(), Request{Team: team, Entry: h.entry, Added: added, Mode: mode, Steps: steps})
	require.NoError(h.t, err)
	return res
}

func (h *harness) mutations() []string {
	return append(append([]string{}, h.gh.mutations...), h.cc.mutations...)
}

func (h *harness) resetMutations() {
	h.gh.mutations, h.cc.mutations = nil, nil
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
	catalog := h.gh.addRepo(owner, "github")
	catalog.files = map[string]string{"catalog/components.yaml": componentYAML("other-service")}
	mapping := h.gh.addRepo(owner, "management-cluster-bases")
	mapping.files = map[string]string{"bases/apps-to-teams-mapping/configmap.yaml": "data:\n  other-service: rocket\n"}
	if inCatalog {
		catalog.files["catalog/components.yaml"] += componentYAML(name)
	}
	if inMapping {
		mapping.files["bases/apps-to-teams-mapping/configmap.yaml"] += "  " + name + ": bumblebee\n"
	}
	h.gh.onDispatch = func(workflow string, _ map[string]any) {
		if workflow == h.baseline.CatalogWorkflow {
			catalog.files["catalog/components.yaml"] += componentYAML(name)
		}
		mapping.files["bases/apps-to-teams-mapping/configmap.yaml"] += "  " + name + ": bumblebee\n"
	}
}

func componentYAML(n string) string {
	return "---\napiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n    name: " + n + "\n"
}

type stepCase struct {
	name  string
	entry string
	added bool
	step  Step
	seed  func(h *harness)
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
			wantCheck: VerdictDrift, wantChange: "render the scaffold and push it as the first commit on main",
			wantAfter: VerdictReported, // the default icon
			verify: func(t *testing.T, h *harness, res *Result) {
				require.Equal(t, scaffoldFiles, h.repo().files)
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
			name: "settings: drift from the baseline", step: StepSettings,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.hasWiki, r.allowSquash, r.allowAuto, r.workflowPerm = true, false, false, "read"
			},
			wantCheck: VerdictDrift, wantChange: "has_wiki true → false, allow_squash_merge false → true, allow_auto_merge false → true",
			verify: func(t *testing.T, h *harness, res *Result) {
				r := h.repo()
				require.False(t, r.hasWiki)
				require.True(t, r.allowSquash && r.allowAuto)
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
			wantCheck: VerdictDrift, wantChange: "protect main; require " + ctxSemantic + ", " + ctxGoBuild,
			verify: func(t *testing.T, h *harness, _ *Result) {
				p := h.repo().protection
				require.NotNil(t, p)
				require.Equal(t, []string{ctxSemantic, ctxGoBuild}, p.checks)
				require.Equal(t, 1, p.reviews)
				require.True(t, p.enforceAdmins && p.strict)
				require.False(t, p.allowForce || p.allowDel)
			},
		},
		{
			name: "protection: ghost contexts are removed", step: StepProtection,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.statuses = []string{ctxGoBuild, ctxSetup, ctxDepGraph}
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: true, checks: []string{ctxGoBuild, ctxSetup, ctxDepGraph, ctxRelease, ctxGhost}}
			},
			wantCheck: VerdictDrift, wantChange: "stop requiring " + strings.Join([]string{ctxSetup, ctxDepGraph, ctxRelease, ctxGhost}, ", "),
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.Equal(t, []string{ctxGoBuild}, h.repo().protection.checks)
			},
		},
		{
			name: "protection: unknown reports remove nothing", step: StepProtection,
			seed: func(h *harness) {
				h.runner.Checks = nil
				r := h.gh.addRepo(owner, name)
				r.protection = &fakeProtection{reviews: 1, enforceAdmins: true, strict: true, checks: []string{ctxGoBuild, "execute-smoke-test"}}
			},
			wantCheck: VerdictOK,
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
			name: "circleci: a followed project without setup workflows and key (template-app's defects)", step: StepCircleCI,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
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
			name: "renovate: an installation without the repository is reported", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.gh.installation.repos = []string{owner + "/other"}
			},
			wantCheck: VerdictReported, wantFinding: FindingRenovateMissing, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				require.Contains(t, res.Findings()[0].Fix, "settings/installations/17164699")
			},
		},
		{
			name: "renovate: an installation on all repositories", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.gh.installation.selection = "all"
			},
			wantCheck: VerdictOK,
		},
		{
			name: "renovate: unreadable with this token", step: StepRenovate,
			seed: func(h *harness) {
				h.gh.addRepo(owner, name)
				h.gh.installation.status = 403
			},
			wantCheck: VerdictSkipped, wantAfter: VerdictSkipped,
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
			wantCheck: VerdictDrift, wantChange: "archive on GitHub; unfollow on CircleCI",
			verify: func(t *testing.T, h *harness, _ *Result) {
				require.True(t, h.repo().archived)
				require.NotContains(t, h.cc.projects, owner+"/"+name)
			},
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
			name: "release: a tag without a pipeline is a missed build and is triggered", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				h.cc.follow(owner, name)
			},
			wantCheck: VerdictDrift, wantChange: "trigger the missed tag build for v0.1.0",
			verify: func(t *testing.T, h *harness, _ *Result) {
				p := h.cc.projects[owner+"/"+name]
				require.Len(t, p.pipelines, 1)
				require.Equal(t, "v0.1.0", p.pipelines[0].VCS.Tag)
			},
		},
		{
			name: "release: a red tag pipeline is reported as a dead tag", step: StepRelease,
			seed: func(h *harness) {
				r := h.gh.addRepo(owner, name)
				r.release, r.releaseAt = "v0.1.0", time.Now().Add(-time.Hour)
				h.cc.addPipeline(h.cc.follow(owner, name), "v0.1.0", "failed")
			},
			wantCheck: VerdictReported, wantFinding: FindingRedRelease, wantAfter: VerdictReported,
			verify: func(t *testing.T, _ *harness, res *Result) {
				f := res.Findings()[0]
				require.Contains(t, f.Message, "push-to-registries-release")
				require.Contains(t, f.Fix, "v0.1.0 is a dead tag")
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yaml := tc.entry
			if yaml == "" {
				yaml = entryYAML
			}
			h := newHarness(t, yaml)
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

func kinds(findings []Finding) []FindingKind {
	out := make([]FindingKind, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
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
	require.Empty(t, h.repo().protection.checks, "nothing has reported on a fresh repository, so nothing is required")
	require.True(t, first.Converged)

	h.resetMutations()
	second := h.run(ModeRepair, true)
	require.True(t, second.Converged, "%+v", second.Steps)
	require.Empty(t, h.mutations())
	for _, sr := range second.Steps {
		require.Contains(t, []Verdict{VerdictOK, VerdictReported}, sr.Verdict, "%s: %+v", sr.Step, sr)
	}
	require.Equal(t, []FindingKind{FindingDefaultIcon}, kinds(second.Findings()))

	data, err := json.Marshal(second)
	require.NoError(t, err)
	require.Contains(t, string(data), `"repository":"giantswarm/sample-service"`)
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

func TestRunRefusesWhatCannotRun(t *testing.T) {
	h := newHarness(t, entryYAML)
	_, err := h.runner.Run(context.Background(), Request{Team: team, Entry: reposetup.Entry{Name: name}})
	require.True(t, IsInvalidConfig(err), "an unaccepted entry: %v", err)
	_, err = h.runner.Run(context.Background(), Request{Team: team, Entry: h.entry, Steps: []Step{"paint"}})
	require.True(t, IsInvalidConfig(err), "an unknown step: %v", err)
	_, err = (&Runner{}).Run(context.Background(), Request{Team: team, Entry: h.entry})
	require.True(t, IsInvalidConfig(err), "no GitHub client: %v", err)
}

func TestRequiredChecks(t *testing.T) {
	b := DefaultBaseline()
	b.RequiredChecks = []string{"PR Gatekeeper"}
	gates := []string{ctxGoBuild, "ci/circleci: build-chart"}

	got, err := requiredChecks(b,
		[]string{ctxGoBuild, ctxSetup, ctxDepGraph, "execute-smoke-test", ctxGhost},
		[]string{ctxGoBuild, ctxSetup, ctxDepGraph, "execute-smoke-test", ctxSemantic, "ci/circleci: build-chart"}, true,
		gates, true)
	require.NoError(t, err)
	require.Equal(t, []string{"PR Gatekeeper", ctxGoBuild, "execute-smoke-test", ctxSemantic, "ci/circleci: build-chart"}, got,
		"unconditional first; the reported hand-added gate stays; the stale CircleCI job, the ignored context and the ghost go; the candidates that reported join")

	got, err = requiredChecks(b, []string{ctxGoBuild, ctxGhost}, nil, false, nil, false)
	require.NoError(t, err)
	require.Equal(t, []string{"PR Gatekeeper", ctxGoBuild, ctxGhost}, got, "unknown reports and pipeline: nothing removed, nothing added")
}
