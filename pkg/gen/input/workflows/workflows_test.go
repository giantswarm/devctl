package workflows

import (
	"bytes"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/renovate"
)

// update regenerates the golden fixtures in testdata/ instead of asserting
// against them. Run `go test ./pkg/gen/input/workflows/... -update` after
// changing a template.
var update = flag.Bool("update", false, "update golden files")

const (
	goldenHelmDocsRegenPath = "testdata/helm-docs-regen.yaml.golden"

	// fixedHeader replaces the real header in golden renders. The real one
	// carries the URL of the last commit that touched the template, which
	// differs between a local checkout and the release build.
	fixedHeader = "# DO NOT EDIT. Generated with:\n#\n#    devctl\n#"
)

// renderInput executes an input.Input the same way pkg/gen/internal.Execute
// does, returning the bytes that would be written to disk.
func renderInput(t *testing.T, in input.Input) string {
	t.Helper()

	tpl := template.New("workflow")
	if in.TemplateDelims.Left != "" {
		tpl = tpl.Delims(in.TemplateDelims.Left, in.TemplateDelims.Right)
	}
	tpl, err := tpl.Parse(in.TemplateBody)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	return rendered.String()
}

// withFixedHeader swaps the provenance header of an input for a constant so
// the render is stable across checkouts.
func withFixedHeader(t *testing.T, in input.Input) input.Input {
	t.Helper()

	data, ok := in.TemplateData.(map[string]interface{})
	if !ok {
		t.Fatalf("TemplateData is %T, want map[string]interface{}", in.TemplateData)
	}
	if _, exists := data["Header"]; !exists {
		t.Fatal("TemplateData has no Header key")
	}
	data["Header"] = fixedHeader

	return in
}

func newWorkflows(t *testing.T, flavours ...gen.Flavour) *Workflows {
	t.Helper()

	w, err := New(Config{Flavours: flavours})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	return w
}

func assertGolden(t *testing.T, golden, got string) {
	t.Helper()

	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil { // #nosec G306 -- test fixture
			t.Fatalf("update golden %s: %v", golden, err)
		}
		return
	}

	want, err := os.ReadFile(golden) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", golden, err)
	}

	if got != string(want) {
		t.Errorf("rendered workflow does not match %s (run with -update to regenerate)\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
	}
}

// Test_HelmDocsRegenPath pins the generated file name: it is what the
// giantswarm/github align-files run writes and what the issue tracker and
// docs refer to.
func Test_HelmDocsRegenPath(t *testing.T) {
	in := newWorkflows(t, gen.FlavourApp).HelmDocsRegen()

	want := filepath.Join(".github", "workflows", "zz_generated.helm-docs-regen.yaml")
	if in.Path != want {
		t.Errorf("path = %q, want %q", in.Path, want)
	}
}

// Test_GoldenHelmDocsRegen pins the exact rendered workflow. The workflow has
// no repo-specific content -- the hooks and tool versions come from the
// consumer's .pre-commit-config.yaml at run time -- so one golden covers every
// repo.
func Test_GoldenHelmDocsRegen(t *testing.T) {
	got := renderInput(t, withFixedHeader(t, newWorkflows(t, gen.FlavourApp).HelmDocsRegen()))

	assertGolden(t, goldenHelmDocsRegenPath, got)
}

// Test_HelmDocsRegenIsValidYAML parses the render as a GitHub Actions
// workflow and checks the properties the design rests on: it fires on pull
// requests only, both jobs drop GITHUB_TOKEN to read-only (the PAT does the
// push), the regen job is gated on the preflight outputs, and concurrent runs
// for one head ref cancel each other.
func Test_HelmDocsRegenIsValidYAML(t *testing.T) {
	got := renderInput(t, newWorkflows(t, gen.FlavourApp).HelmDocsRegen())

	var wf struct {
		Name string `yaml:"name"`
		On   struct {
			PullRequest struct {
				Paths []string `yaml:"paths"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			If          string            `yaml:"if"`
			Needs       string            `yaml:"needs"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Name string            `yaml:"name"`
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
	}

	if wf.Name != "helm-docs-regen" {
		t.Errorf("name = %q, want helm-docs-regen", wf.Name)
	}
	if len(wf.On.PullRequest.Paths) == 0 || wf.On.PullRequest.Paths[0] != "helm/**" {
		t.Errorf("pull_request paths = %v, want helm/** first", wf.On.PullRequest.Paths)
	}
	if len(wf.Permissions) != 0 {
		t.Errorf("top-level permissions = %v, want none (each job sets its own)", wf.Permissions)
	}
	if wf.Concurrency.Group != "helm-docs-regen-${{ github.head_ref }}" || !wf.Concurrency.CancelInProgress {
		t.Errorf("concurrency = %+v, want a per-head-ref group with cancel-in-progress", wf.Concurrency)
	}

	preflight, ok := wf.Jobs["preflight"]
	if !ok {
		t.Fatalf("no preflight job in %v", wf.Jobs)
	}
	for _, prefix := range []string{"renovate/", "dependabot/"} {
		if !strings.Contains(preflight.If, "startsWith(github.head_ref, '"+prefix+"')") {
			t.Errorf("preflight if = %q, want a startsWith guard for %s branches", preflight.If, prefix)
		}
	}

	regen, ok := wf.Jobs["regen"]
	if !ok {
		t.Fatalf("no regen job in %v", wf.Jobs)
	}
	if regen.Needs != "preflight" {
		t.Errorf("regen needs = %q, want preflight", regen.Needs)
	}
	for _, want := range []string{"needs.preflight.outputs.token-available == 'true'", "needs.preflight.outputs.hooks != ''"} {
		if !strings.Contains(regen.If, want) {
			t.Errorf("regen if = %q, missing %q", regen.If, want)
		}
	}
	for name, job := range wf.Jobs {
		if job.Permissions["contents"] != "read" {
			t.Errorf("job %s permissions = %v, want contents: read only", name, job.Permissions)
		}
	}

	var checkout, push bool
	for _, step := range regen.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@") {
			checkout = true
			if step.With["token"] != "${{ steps.release-token.outputs.token }}" {
				t.Errorf("regen checkout token = %q, want the giantswarm-release App token (a GITHUB_TOKEN push does not re-trigger the required checks)", step.With["token"])
			}
			if step.With["ref"] != "${{ github.head_ref }}" {
				t.Errorf("regen checkout ref = %q, want the head branch so the commit lands on it", step.With["ref"])
			}
		}
		if strings.Contains(step.Run, "git push") {
			push = true
			if !strings.Contains(step.Run, `git status --porcelain`) {
				t.Errorf("push step must no-op on a clean tree:\n%s", step.Run)
			}
		}
	}
	if !checkout {
		t.Error("regen job has no actions/checkout step")
	}
	if !push {
		t.Error("regen job has no push step")
	}
}

// Test_HelmDocsRegenRunsHooksTwice pins the convergence guard: every hook runs
// once tolerating a rewrite and once more that has to be clean, so a hook
// that keeps rewriting (or errors out) fails the job instead of pushing.
func Test_HelmDocsRegenRunsHooksTwice(t *testing.T) {
	got := renderInput(t, newWorkflows(t, gen.FlavourApp).HelmDocsRegen())

	tolerant := regexp.MustCompile(`pre-commit run "\$hook" --all-files \|\| true`)
	strict := regexp.MustCompile(`pre-commit run "\$hook" --all-files\n`)

	if len(tolerant.FindAllString(got, -1)) != 1 {
		t.Errorf("want exactly one tolerant pre-commit pass:\n%s", got)
	}
	if len(strict.FindAllString(got, -1)) != 1 {
		t.Errorf("want exactly one strict pre-commit pass:\n%s", got)
	}
	if strings.Index(got, tolerant.FindString(got)) > strings.Index(got, strict.FindString(got)) {
		t.Errorf("the tolerant pass must run before the strict one:\n%s", got)
	}
}

// Test_HelmDocsRegenNoExpressionInRun guards against GitHub Actions template
// injection: every `${{ }}` expression must be passed through `env:` or
// `with:`, never interpolated into a `run:` script.
func Test_HelmDocsRegenNoExpressionInRun(t *testing.T) {
	got := renderInput(t, newWorkflows(t, gen.FlavourApp).HelmDocsRegen())

	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
	}

	for name, job := range wf.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "${{") {
				t.Errorf("job %s step %q interpolates an expression into its run script:\n%s", name, step.Name, step.Run)
			}
		}
	}
}

// Test_HelmDocsRegenAuthorIsIgnoredByRenovate pins the contract between the
// workflow and the generated renovate.json5: the email the regen commit is
// made with must be listed in gitIgnoredAuthors, or Renovate treats the branch
// as human-edited, stops rebasing it and retitles the PR "- abandoned" instead
// of autoclosing it.
func Test_HelmDocsRegenAuthorIsIgnoredByRenovate(t *testing.T) {
	workflow := renderInput(t, newWorkflows(t, gen.FlavourApp).HelmDocsRegen())

	email := regexp.MustCompile(`git config --local user\.email "([^"]+)"`).FindStringSubmatch(workflow)
	if len(email) != 2 {
		t.Fatalf("workflow sets no git author email:\n%s", workflow)
	}

	r, err := renovate.New(renovate.Config{Language: "go"})
	if err != nil {
		t.Fatalf("renovate.New() returned unexpected error: %v", err)
	}
	renovateConfig := renderInput(t, r.CreateRenovate())

	if !strings.Contains(renovateConfig, "gitIgnoredAuthors: [\n    '"+email[1]+"',") {
		t.Errorf("generated renovate.json5 does not ignore the regen author %q:\n%s", email[1], renovateConfig)
	}
}

// Test_ChartTestsPath pins the generated file name: it is what the
// giantswarm/github align-files run writes into every app repository.
func Test_ChartTestsPath(t *testing.T) {
	in := newWorkflows(t, gen.FlavourApp).ChartTests()

	want := filepath.Join(".github", "workflows", "zz_generated.chart_tests.yaml")
	if in.Path != want {
		t.Errorf("path = %q, want %q", in.Path, want)
	}
}

// Test_ChartTestsIsValidYAML checks the properties the design rests on: the
// workflow fires on pull requests, it delegates to the shared reusable
// workflow, and it grants the job read-only contents access.
func Test_ChartTestsIsValidYAML(t *testing.T) {
	got := renderInput(t, newWorkflows(t, gen.FlavourApp).ChartTests())

	var wf struct {
		Name string `yaml:"name"`
		On   struct {
			PullRequest struct {
				Branches []string `yaml:"branches"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Uses        string            `yaml:"uses"`
			Permissions map[string]string `yaml:"permissions"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
	}

	if wf.Name != "chart-tests" {
		t.Errorf("name = %q, want chart-tests", wf.Name)
	}
	if len(wf.On.PullRequest.Branches) == 0 {
		t.Errorf("pull_request branches = %v, want main and master", wf.On.PullRequest.Branches)
	}
	if len(wf.Permissions) != 0 {
		t.Errorf("top-level permissions = %v, want none (each job sets its own)", wf.Permissions)
	}

	job, ok := wf.Jobs["chart-tests"]
	if !ok {
		t.Fatalf("no chart-tests job in %v", wf.Jobs)
	}
	want := "giantswarm/github-workflows/.github/workflows/chart-tests.yaml@main"
	if job.Uses != want {
		t.Errorf("uses = %q, want %q", job.Uses, want)
	}
	if job.Permissions["contents"] != "read" {
		t.Errorf("job permissions = %v, want contents: read", job.Permissions)
	}
}

// Test_ReservationCommandIsValidYAML checks the properties the design rests
// on: the workflow fires on new comments and on closed pull requests, a job
// filter skips other comments, the job has no GITHUB_TOKEN permissions, and
// the app token it mints is scoped to dispatching to mc-reservations.
func Test_ReservationCommandIsValidYAML(t *testing.T) {
	in := newWorkflows(t, gen.FlavourApp).ReservationCommand()
	if want := filepath.Join(".github", "workflows", "zz_generated.reservation_command.yaml"); in.Path != want {
		t.Errorf("path = %q, want %q", in.Path, want)
	}
	got := renderInput(t, in)

	var wf struct {
		On struct {
			IssueComment struct {
				Types []string `yaml:"types"`
			} `yaml:"issue_comment"`
			PullRequest struct {
				Types []string `yaml:"types"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
	}

	if !slices.Equal(wf.On.IssueComment.Types, []string{"created"}) {
		t.Errorf("issue_comment types = %v, want [created]", wf.On.IssueComment.Types)
	}
	if !slices.Equal(wf.On.PullRequest.Types, []string{"closed"}) {
		t.Errorf("pull_request types = %v, want [closed]", wf.On.PullRequest.Types)
	}
	if len(wf.Permissions) != 0 {
		t.Errorf("top-level permissions = %v, want none", wf.Permissions)
	}

	job, ok := wf.Jobs["dispatch"]
	if !ok {
		t.Fatalf("no dispatch job in %v", wf.Jobs)
	}
	if len(job.Permissions) != 0 {
		t.Errorf("job permissions = %v, want none", job.Permissions)
	}
	if !strings.Contains(job.If, "'/deploy'") || !strings.Contains(job.If, "head.repo.full_name == github.repository") {
		t.Errorf("job if does not filter commands and forks:\n%s", job.If)
	}
	if len(job.Steps) == 0 || !strings.HasPrefix(job.Steps[0].Uses, "actions/create-github-app-token@") {
		t.Fatalf("first step does not mint an app token: %+v", job.Steps)
	}
	with := job.Steps[0].With
	for k, want := range map[string]string{
		"client-id":           "${{ secrets.MC_RESERVATIONS_DISPATCH_CLIENT_ID }}",
		"private-key":         "${{ secrets.MC_RESERVATIONS_DISPATCH_APP_KEY }}",
		"repositories":        "mc-reservations",
		"permission-contents": "write",
	} {
		if with[k] != want {
			t.Errorf("app token %s = %q, want %q", k, with[k], want)
		}
	}
}

func Test_ReservationCommandGolden(t *testing.T) {
	got := renderInput(t, withFixedHeader(t, newWorkflows(t, gen.FlavourApp).ReservationCommand()))
	assertGolden(t, "testdata/reservation_command.yaml.golden", got)
}

// Test_ValidateOrganizationKustomizations checks what a required check needs: the
// workflow runs on every pull request (a path filter would leave the check pending on
// the others), the job keeps its name (the required context), and the check's code
// comes from management-cluster-bases, not from the pull request.
func Test_ValidateOrganizationKustomizations(t *testing.T) {
	in := newWorkflows(t, gen.FlavourManagementClustersFleet).ValidateOrganizationKustomizations()
	if want := filepath.Join(".github", "workflows", "zz_generated.validate_organization_kustomizations.yaml"); in.Path != want {
		t.Errorf("path = %q, want %q", in.Path, want)
	}
	got := renderInput(t, in)

	var wf struct {
		On          []string          `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Name        string            `yaml:"name"`
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Uses string `yaml:"uses"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
	}

	if !slices.Equal(wf.On, []string{"pull_request", "merge_group"}) {
		t.Errorf("on = %v, want [pull_request merge_group] with no filter", wf.On)
	}
	if len(wf.Permissions) != 0 {
		t.Errorf("top-level permissions = %v, want none", wf.Permissions)
	}
	job, ok := wf.Jobs["check"]
	if !ok || len(wf.Jobs) != 1 {
		t.Fatalf("jobs = %v, want the one job check", wf.Jobs)
	}
	if want := "Organization kustomizations stay in their directory"; job.Name != want {
		t.Errorf("job name = %q, want %q: it is the required check's context", job.Name, want)
	}
	if job.If != "" {
		t.Errorf("job if = %q: a skipped job leaves the required check pending", job.If)
	}
	if !maps.Equal(job.Permissions, map[string]string{"contents": "read"}) {
		t.Errorf("job permissions = %v, want contents: read", job.Permissions)
	}
	const action = "giantswarm/management-cluster-bases/.github/actions/organization-kustomizations@main"
	var uses []string
	for _, s := range job.Steps {
		if s.Run != "" {
			t.Errorf("step runs %q: the check's code must come from management-cluster-bases", s.Run)
		}
		uses = append(uses, s.Uses)
	}
	if len(uses) != 2 || !strings.HasPrefix(uses[0], "actions/checkout@") || uses[1] != action {
		t.Errorf("steps use %v, want actions/checkout, then %s", uses, action)
	}
}

func Test_ValidateOrganizationKustomizationsGolden(t *testing.T) {
	got := renderInput(t, withFixedHeader(t, newWorkflows(t, gen.FlavourManagementClustersFleet).ValidateOrganizationKustomizations()))
	assertGolden(t, "testdata/validate_organization_kustomizations.yaml.golden", got)
}

// Test_PullRequestWorkflowsConcurrency verifies that generated pull request
// workflows group runs by workflow and pull request number (falling back to
// ref) and cancel in-progress runs only for pull_request events, that the
// semantic-pull-request workflow cancels nothing, and that release workflows
// retain their non-cancelling per-ref serialization.
func Test_PullRequestWorkflowsConcurrency(t *testing.T) {
	w := newWorkflows(t, gen.FlavourApp)

	prWorkflows := []struct {
		name  string
		input input.Input
	}{
		{name: "chart-tests", input: w.ChartTests()},
		{name: "check-values-schema", input: w.CheckValuesSchema()},
		{name: "check-readme-links", input: w.CheckReadmeLinks()},
		{name: "gitleaks", input: w.Gitleaks()},
	}

	wantGroup := "${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
	wantCancel := "${{ github.event_name == 'pull_request' }}"

	for _, tc := range prWorkflows {
		t.Run(tc.name, func(t *testing.T) {
			got := renderInput(t, tc.input)

			var wf struct {
				Concurrency struct {
					Group            string `yaml:"group"`
					CancelInProgress string `yaml:"cancel-in-progress"`
				} `yaml:"concurrency"`
			}
			if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
				t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
			}

			if wf.Concurrency.Group != wantGroup {
				t.Errorf("concurrency.group = %q, want %q", wf.Concurrency.Group, wantGroup)
			}
			if wf.Concurrency.CancelInProgress != wantCancel {
				t.Errorf("concurrency.cancel-in-progress = %q, want %q", wf.Concurrency.CancelInProgress, wantCancel)
			}
		})
	}

	t.Run("semantic-pull-request", func(t *testing.T) {
		got := renderInput(t, w.SemanticPullRequest())

		var wf map[string]any
		if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
			t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
		}
		// A push and a description edit run the title check twice for the
		// same head; a cancelled run would stay on the commit and turn its
		// check rollup red.
		if c, ok := wf["concurrency"]; ok {
			t.Errorf("concurrency = %v, want none", c)
		}
	})

	releaseWorkflows := []struct {
		name  string
		input input.Input
	}{
		{name: "auto-release", input: w.AutoRelease()},
		{name: "auto-release-multi-image", input: newWorkflows(t, gen.FlavourMultiImage).AutoReleaseMultiImage()},
	}

	for _, tc := range releaseWorkflows {
		t.Run(tc.name, func(t *testing.T) {
			got := renderInput(t, tc.input)

			var wf struct {
				Concurrency struct {
					Group            string `yaml:"group"`
					CancelInProgress string `yaml:"cancel-in-progress"`
				} `yaml:"concurrency"`
			}
			if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
				t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
			}

			if wf.Concurrency.Group != "auto-release-${{ github.ref_name }}" {
				t.Errorf("concurrency.group = %q, want %q", wf.Concurrency.Group, "auto-release-${{ github.ref_name }}")
			}
			if wf.Concurrency.CancelInProgress != "false" {
				t.Errorf("concurrency.cancel-in-progress = %q, want false", wf.Concurrency.CancelInProgress)
			}
		})
	}
}
