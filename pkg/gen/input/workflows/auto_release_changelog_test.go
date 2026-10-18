package workflows

import (
	"fmt"
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

// Test_AutoReleaseChangelogOpenStep runs the open step with a stand-in gh that
// logs every call with the token it ran under: taylorbot opens the cut and
// arms auto-merge, and architectbot, a second identity, approves it, so the
// review the branch requires never waits for a person. The approval is scoped
// to the cut the step pushed: a pull request another account authored, whose
// head moved, or that changes any file beside CHANGELOG.md gets none. Without
// the approver's secret the step warns and still succeeds.
func Test_AutoReleaseChangelogOpenStep(t *testing.T) {
	job := workflowJobs(t, renderInput(t, newWorkflows(t, gen.FlavourApp).AutoRelease()))["changelog"]
	var step autoReleaseStep
	for _, s := range job.Steps {
		if s.Name == "Open the cut as a pull request" {
			step = s
		}
	}
	for v, want := range map[string]string{ // #nosec G101 -- secret names, not values
		"GH_TOKEN":       "${{ secrets.TAYLORBOT_GITHUB_ACTION }}",
		"APPROVER_TOKEN": "${{ secrets.ARCHITECT_GITHUB_REPO_SYNC_TOKEN }}",
	} {
		if got := step.Env[v]; got != want {
			t.Fatalf("open step: env %s = %q, want %q", v, got, want)
		}
	}

	const notApproved = "::warning title=Cut not approved::"
	for _, tc := range []struct {
		name     string
		approver string
		// The pull request gh reports: its author, the revision its head is
		// (HEAD is the cut commit) and its files; empty is the cut's own.
		author, head, files string
		// lag is how many reads report the previous head before the cut's;
		// until then pr merge refuses the cut as the expected head.
		lag      int
		approved bool
		warning  string
	}{
		{name: "the cut, approved by a second identity", approver: "architect", approved: true},
		{
			name: "no approver secret", warning: notApproved + "no ARCHITECT_GITHUB_REPO_SYNC_TOKEN secret",
		},
		{
			name: "another file beside the cut", approver: "architect", files: "CHANGELOG.md,main.go",
			warning: notApproved + "https://github.com/o/r/pull/1 is not the cut alone",
		},
		{
			name: "another author", approver: "architect", author: "someone",
			warning: notApproved + "https://github.com/o/r/pull/1 is not the cut alone",
		},
		{
			name: "the head reported late, read again", approver: "architect", lag: 2, approved: true,
		},
		{
			name: "the head moved after the push", approver: "architect", head: "HEAD~1",
			warning: notApproved + "https://github.com/o/r/pull/1 is not the cut alone",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			log := filepath.Join(bin, "log")
			// gh's pr view reports HEAD~1 for its first $PR_LAG reads; sleep
			// only returns.
			gh := fmt.Sprintf("#!/bin/sh\necho \"$GH_TOKEN $*\" >> %[1]q\ncase \"$1 $2\" in\n"+
				"'pr create') echo https://github.com/o/r/pull/1 ;;\n"+
				// Like GitHub, pr merge refuses an expected head it does not
				// report yet.
				"'pr merge') if [ \"$(grep -c 'pr view' %[1]q)\" -le %[2]d ]; then echo 'expected head oid does not match' >&2; exit 1; fi ;;\n"+
				"'pr view') n=$(grep -c 'pr view' %[1]q); head=${PR_HEAD:-HEAD}; [ \"$n\" -le %[2]d ] && head=HEAD~1\n"+
				"  echo \"${PR_AUTHOR:-taylorbot} $(git rev-parse \"$head\") ${PR_FILES:-CHANGELOG.md}\" ;;\n"+
				"'api user') echo taylorbot ;;\nesac\n", log, tc.lag)
			for file, content := range map[string]string{"gh": gh, "sleep": "#!/bin/sh\n"} {
				if err := os.WriteFile(filepath.Join(bin, file), []byte(content), 0o700); err != nil { // #nosec G306 -- test stand-in
					t.Fatal(err)
				}
			}
			origin := t.TempDir()
			mustGit(t, origin, "init", "--bare")
			dir := repo(t)
			mustGit(t, dir, "remote", "add", "origin", origin)
			if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			mustGit(t, dir, "add", "CHANGELOG.md")
			mustGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-m", "docs: changelog")
			if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog\ncut\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			mustGit(t, dir, "config", "--local", "user.name", "taylorbot")
			mustGit(t, dir, "config", "--local", "user.email", "dev@giantswarm.io")

			_, out, err := runStep(t, step.Run, dir, "PATH="+bin+":"+os.Getenv("PATH"),
				"GH_TOKEN=taylor", "APPROVER_TOKEN="+tc.approver, "TAG=v1.1.0-rc.1", "GITHUB_REPOSITORY=o/r",
				"PR_AUTHOR="+tc.author, "PR_HEAD="+tc.head, "PR_FILES="+tc.files)
			if err != nil {
				t.Fatalf("err = %v:\n%s", err, out)
			}
			raw, err := os.ReadFile(log) // #nosec G304 -- path built from t.TempDir
			if err != nil {
				t.Fatal(err)
			}
			calls := string(raw)
			// The auto-merge and the approval are pinned to the cut commit,
			// so a later push to the branch merges nothing on them.
			sha := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD"))
			for _, want := range []string{
				"taylor pr create",
				"taylor pr merge https://github.com/o/r/pull/1 --auto --squash --match-head-commit " + sha,
			} {
				if !strings.Contains(calls, want) {
					t.Errorf("no gh call %q:\n%s", want, calls)
				}
			}
			approval := "architect api --silent -X POST repos/o/r/pulls/1/reviews -f event=APPROVE -f commit_id=" + sha
			if got := strings.Contains(calls, approval); got != tc.approved {
				t.Errorf("approved = %t, want %t:\n%s", got, tc.approved, calls)
			}
			if tc.approved && strings.Contains(out, "::warning") {
				t.Errorf("the approved cut warns:\n%s", out)
			}
			if !strings.Contains(out, tc.warning) {
				t.Errorf("output lacks %q:\n%s", tc.warning, out)
			}
		})
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

// Test_ChangelogStepsWaitForDevctlBinary runs the download of both steps that
// run devctl against a gh that finds no assets for its first failures, as for
// a devctl release whose binaries CircleCI has not uploaded yet: the step
// retries, and fails saying why once the assets never come.
func Test_ChangelogStepsWaitForDevctlBinary(t *testing.T) {
	steps := map[string]autoReleaseStep{
		"cut": jobStep(t, workflowJobs(t, renderInput(t, newWorkflows(t, gen.FlavourApp).AutoRelease()))["changelog"], "cut"),
	}
	check := workflowJobs(t, renderInput(t, newWorkflows(t, gen.FlavourApp).Changelog()))["released-entries"]
	steps["check"] = check.Steps[len(check.Steps)-1]

	for name, step := range steps {
		for _, tc := range []struct {
			failures int
			wantErr  bool
		}{
			{failures: 0},
			{failures: 3},
			{failures: 20, wantErr: true},
		} {
			t.Run(fmt.Sprintf("%s/%d failures", name, tc.failures), func(t *testing.T) {
				bin := t.TempDir()
				count := filepath.Join(bin, "count")
				// gh fails $failures times, then "downloads" a devctl that
				// succeeds; sleep only counts.
				gh := fmt.Sprintf("#!/bin/sh\nn=$(cat %[1]q 2>/dev/null || echo 0); echo $((n+1)) > %[1]q\n"+
					"if [ \"$n\" -lt %[2]d ]; then echo 'no assets to download' >&2; exit 1; fi\n"+
					"while [ $# -gt 0 ]; do [ \"$1\" = --output ] && out=$2; shift; done\nprintf '#!/bin/sh\\n' > \"$out\"\n", count, tc.failures)
				for file, content := range map[string]string{"gh": gh, "sleep": "#!/bin/sh\n"} {
					if err := os.WriteFile(filepath.Join(bin, file), []byte(content), 0o700); err != nil { // #nosec G306 -- test stand-in
						t.Fatal(err)
					}
				}
				dir := repo(t)
				if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog\n"), 0o600); err != nil {
					t.Fatal(err)
				}

				_, out, err := runStep(t, step.Run, dir, "PATH="+bin+":"+os.Getenv("PATH"), "RUNNER_TEMP="+t.TempDir())
				if tc.wantErr {
					if err == nil || !strings.Contains(out, "has no devctl-linux-amd64 after 10 minutes") {
						t.Fatalf("err = %v, want the step to fail saying why:\n%s", err, out)
					}
					return
				}
				if err != nil {
					t.Fatalf("err = %v:\n%s", err, out)
				}
				if got := strings.Count(out, "retrying in 30s"); got != tc.failures {
					t.Fatalf("%d retries, want %d:\n%s", got, tc.failures, out)
				}
			})
		}
	}
}
