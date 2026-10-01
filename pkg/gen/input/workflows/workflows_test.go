package workflows

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
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
			if step.With["token"] != "${{ secrets.TAYLORBOT_GITHUB_ACTION }}" {
				t.Errorf("regen checkout token = %q, want the taylorbot PAT (a GITHUB_TOKEN push does not re-trigger the required checks)", step.With["token"])
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
