package workflows

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

type workflowJob struct {
	Needs   string            `yaml:"needs"`
	If      string            `yaml:"if"`
	Outputs map[string]string `yaml:"outputs"`
	Steps   []autoReleaseStep `yaml:"steps"`
}

func workflowJobs(t *testing.T, rendered string) map[string]workflowJob {
	t.Helper()

	var wf struct {
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, rendered)
	}

	return wf.Jobs
}

func jobStep(t *testing.T, job workflowJob, id string) autoReleaseStep {
	t.Helper()

	for _, s := range job.Steps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no step with id %q", id)

	return autoReleaseStep{}
}

// Test_AutoReleaseChangelogJob pins that every release opens the CHANGELOG
// cut as a pull request after the tag job cut a release, under a `docs`
// subject git-cliff skips so its merge releases nothing, and that a fork line,
// whose CHANGELOG.md is upstream's, has no such job.
func Test_AutoReleaseChangelogJob(t *testing.T) {
	rendered := renderInput(t, newWorkflows(t, gen.FlavourApp).AutoRelease())
	jobs := workflowJobs(t, rendered)

	if got := jobs["tag"].Outputs["tag"]; got != "${{ steps.decide.outputs.tag }}" {
		t.Fatalf("tag job output tag = %q", got)
	}
	job, ok := jobs["changelog"]
	if !ok {
		t.Fatalf("no changelog job:\n%s", rendered)
	}
	if job.Needs != "tag" || job.If != "needs.tag.outputs.tag != ''" {
		t.Fatalf("changelog job runs on needs=%q if=%q, want after a tag only", job.Needs, job.If)
	}
	if !strings.Contains(jobStep(t, job, "cut").Run, `"${RUNNER_TEMP}/devctl" changelog cut`) {
		t.Fatal("the cut step does not run devctl changelog cut")
	}

	open := ""
	for _, s := range job.Steps {
		if s.Name == "Open the cut as a pull request" {
			open = s.Run
		}
	}
	title := `title="docs(changelog): `
	if !strings.Contains(open, title) {
		t.Fatalf("the pull request title is not %s…:\n%s", title, open)
	}
	if !slices.Contains(cliffSkippedTypes, "docs") {
		t.Fatal("cliff.toml releases docs commits: the cut pull request would cut a release of its own")
	}
	for _, want := range []string{"gh pr create", "gh pr merge \"$pr\" --auto --squash"} {
		if !strings.Contains(open, want) {
			t.Fatalf("the open step does not run %s", want)
		}
	}

	fork := workflowJobs(t, renderInput(t, newWorkflows(t, gen.FlavourFork).AutoRelease()))
	if _, ok := fork["changelog"]; ok {
		t.Fatal("a fork line has a changelog job")
	}
	if fork["tag"].Outputs != nil {
		t.Fatal("a fork line's tag job has outputs")
	}
}

// Test_AutoReleaseChangelogCutStep runs the cut step with stand-ins for gh and
// devctl: it reports a changed CHANGELOG.md, and a repository without one is
// left alone without downloading devctl.
func Test_AutoReleaseChangelogCutStep(t *testing.T) {
	job := workflowJobs(t, renderInput(t, newWorkflows(t, gen.FlavourApp).AutoRelease()))["changelog"]
	step := jobStep(t, job, "cut")
	assertDevctlAuthenticated(t, step)
	script := step.Run

	// gh "downloads" a devctl that rewrites CHANGELOG.md.
	bin := t.TempDir()
	gh := "#!/bin/sh\nwhile [ $# -gt 0 ]; do [ \"$1\" = --output ] && out=$2; shift; done\nprintf '#!/bin/sh\\necho cut >> CHANGELOG.md\\n' > \"$out\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o700); err != nil { // #nosec G306 -- test stand-in
		t.Fatal(err)
	}
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "RUNNER_TEMP=" + t.TempDir()}

	dir := repo(t)
	got, out, err := runStep(t, script, dir, env...)
	if err != nil || got["changed"] != "false" {
		t.Fatalf("without CHANGELOG.md: changed=%q err=%v\n%s", got["changed"], err, out)
	}

	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "CHANGELOG.md")
	mustGit(t, dir, "commit", "-m", "docs: changelog")
	got, out, err = runStep(t, script, dir, env...)
	if err != nil || got["changed"] != "true" {
		t.Fatalf("with a cut: changed=%q err=%v\n%s", got["changed"], err, out)
	}
}

// assertDevctlAuthenticated fails unless a step that downloads and runs
// devctl hands it the job's token: gh reads $GH_TOKEN, and devctl's version
// check, which reads devctl's releases before every command, reads
// $GITHUB_TOKEN. Without it the check is anonymous and shares the runner IP's
// rate limit.
func assertDevctlAuthenticated(t *testing.T, step autoReleaseStep) {
	t.Helper()

	for _, v := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if got := step.Env[v]; got != "${{ secrets.GITHUB_TOKEN }}" {
			t.Errorf("step %q: env %s = %q, want the job's token", step.Name, v, got)
		}
	}
}

// Test_ChangelogWorkflow pins the pull request check: devctl changelog check
// on a checkout with every tag.
func Test_ChangelogWorkflow(t *testing.T) {
	in := newWorkflows(t, gen.FlavourApp).Changelog()
	if in.Path != ".github/workflows/zz_generated.changelog.yaml" {
		t.Fatalf("path = %q", in.Path)
	}
	rendered := renderInput(t, in)
	job, ok := workflowJobs(t, rendered)["released-entries"]
	if !ok {
		t.Fatalf("no released-entries job:\n%s", rendered)
	}
	if !strings.Contains(rendered, "fetch-depth: 0") || !strings.Contains(rendered, "fetch-tags: true") {
		t.Fatal("the check does not see every tag")
	}
	last := job.Steps[len(job.Steps)-1]
	if !strings.Contains(last.Run, `"${RUNNER_TEMP}/devctl" changelog check`) {
		t.Fatalf("the check step does not run devctl changelog check:\n%s", last.Run)
	}
	assertDevctlAuthenticated(t, last)
	if del := newWorkflows(t, gen.FlavourApp).ChangelogDeletion(); !del.Delete || del.Path != in.Path {
		t.Fatalf("ChangelogDeletion = %+v", del)
	}
}
