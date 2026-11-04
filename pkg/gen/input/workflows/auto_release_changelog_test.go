package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// changelogWorkflow is the part of the rendered workflow the changelog job's
// wiring is read from.
type changelogWorkflow struct {
	Jobs map[string]struct {
		If              string            `yaml:"if"`
		Needs           string            `yaml:"needs"`
		ContinueOnError bool              `yaml:"continue-on-error"`
		Outputs         map[string]string `yaml:"outputs"`
		Steps           []autoReleaseStep `yaml:"steps"`
	} `yaml:"jobs"`
}

func renderedChangelogWorkflow(t *testing.T, flavours ...gen.Flavour) changelogWorkflow {
	t.Helper()

	rendered := renderInput(t, newWorkflows(t, flavours...).AutoRelease())
	var wf changelogWorkflow
	if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, rendered)
	}

	return wf
}

// changelogStep returns the script of the changelog job's step with that name.
func changelogStep(t *testing.T, name string) string {
	t.Helper()

	for _, s := range renderedChangelogWorkflow(t, gen.FlavourApp).Jobs["changelog"].Steps {
		if s.Name == name {
			return s.Run
		}
	}
	t.Fatalf("no step %q in the changelog job", name)

	return ""
}

const (
	writeChangelogStep = "Write the release into CHANGELOG.md"
	openChangelogStep  = "Open the changelog pull request"
)

// changelogRepo is a clone of a bare origin, as the job's checkout leaves it,
// with a stub gh on PATH. The stub records its calls in calls, answers
// `release view <tag>` from releases and `pr list` with open, as gh would at the
// position and limit the call gives.
type changelogRepo struct {
	dir, origin, bin, calls string
}

func newChangelogRepo(t *testing.T, changelog string, open string, releases map[string]string) changelogRepo {
	t.Helper()

	root := t.TempDir()
	r := changelogRepo{
		dir:    filepath.Join(root, "repo"),
		origin: filepath.Join(root, "origin.git"),
		bin:    filepath.Join(root, "bin"),
		calls:  filepath.Join(root, "gh-calls"),
	}
	mustGit(t, root, "init", "--bare", "--initial-branch=main", r.origin)
	mustGit(t, root, "clone", "-q", r.origin, r.dir)
	mustGit(t, r.dir, "switch", "-q", "-c", "main")
	if changelog != "" {
		writeChangelog(t, r.dir, changelog)
		mustGit(t, r.dir, "add", "CHANGELOG.md")
	}
	mustGit(t, r.dir, "commit", "-q", "--allow-empty", "-m", "chore: inception")
	mustGit(t, r.dir, "push", "-q", "origin", "main")

	notes := filepath.Join(root, "releases")
	for _, dir := range []string{r.bin, notes} {
		if err := os.Mkdir(dir, 0o750); err != nil {
			t.Fatalf("make %s: %v", dir, err)
		}
	}
	for tag, body := range releases {
		raw, err := json.Marshal(map[string]string{"body": body, "publishedAt": "2026-11-04T08:00:00Z"})
		if err != nil {
			t.Fatalf("marshal release: %v", err)
		}
		if err := os.WriteFile(filepath.Join(notes, tag), raw, 0o600); err != nil {
			t.Fatalf("write release: %v", err)
		}
	}

	stub := fmt.Sprintf(`#!/usr/bin/env bash
echo "$*" >> %q
case "$1 $2" in
  "pr list")
    # gh returns the newest 30 unless told otherwise, and --jq filters only
    # those: the pull requests sit at GH_OPEN_POSITION in that list.
    limit=30
    args=("$@")
    for i in "${!args[@]}"; do
      if [ "${args[$i]}" = --limit ]; then limit=${args[$((i + 1))]}; fi
    done
    if [ "${GH_OPEN_POSITION:-1}" -le "$limit" ]; then
      echo %q | sed '/^$/d'
    fi
    ;;
  "release view") cat %q/"$3" ;;
  "pr create") echo https://github.com/giantswarm/x/pull/9 ;;
  "pr merge") exit "${GH_MERGE_EXIT:-0}" ;;
esac
`, r.calls, open, notes)
	if err := os.WriteFile(filepath.Join(r.bin, "gh"), []byte(stub), 0o700); err != nil { // #nosec G306 -- the stub has to be executable
		t.Fatalf("write gh stub: %v", err)
	}

	return r
}

func writeChangelog(t *testing.T, dir, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write CHANGELOG.md: %v", err)
	}
}

// release commits on origin's main and tags it, as a merged pull request and
// the tag job do.
func (r changelogRepo) release(t *testing.T, tags ...string) {
	t.Helper()

	for _, tag := range tags {
		mustGit(t, r.dir, "commit", "-q", "--allow-empty", "-m", "fix: for "+tag)
		mustGit(t, r.dir, "tag", tag)
	}
	mustGit(t, r.dir, "push", "-q", "--tags", "origin", "main")
}

func (r changelogRepo) run(t *testing.T, step, tag string, env ...string) (map[string]string, string, error) {
	t.Helper()

	env = append(env,
		"PATH="+r.bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GH_REPO=giantswarm/x", "TAG="+tag,
	)

	return runStep(t, changelogStep(t, step), r.dir, env...)
}

func (r changelogRepo) changelog(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(r.dir, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}

	return string(raw)
}

func (r changelogRepo) ghCalls(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(r.calls)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read gh calls: %v", err)
	}

	return string(raw)
}

const changelogHead = "# Changelog\n\nAll notable changes.\n\n"

// inOrder fails unless every part occurs in got, one after the other.
func inOrder(t *testing.T, got string, parts ...string) {
	t.Helper()

	rest := got
	for _, part := range parts {
		_, after, found := strings.Cut(rest, part)
		if !found {
			t.Fatalf("%q missing or out of order in:\n%s", part, got)
		}
		rest = after
	}
}

// Test_AutoReleaseChangelogSection pins what a stable release writes into
// CHANGELOG.md, whatever shape the file arrives in.
func Test_AutoReleaseChangelogSection(t *testing.T) {
	notes := "### Fixed\n\n- A fix\n\n\n**Full Changelog**: compare\n"
	marker := "<!-- Written at release from the pull request titles. Pull requests do not edit this file. -->"

	t.Run("the section lands under Unreleased, above the older ones", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n\n- old\n", "",
			map[string]string{"v1.0.1": notes})
		r.release(t, "v1.0.0", "v1.0.1")

		got, out, err := r.run(t, writeChangelogStep, "v1.0.1")
		if err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		if got["changed"] != "true" {
			t.Fatalf("changed = %q, want true\n%s", got["changed"], out)
		}
		inOrder(t, r.changelog(t), "# Changelog", "## [Unreleased]", marker, "## [1.0.1] - 2026-11-04", "### Fixed", "- A fix", "**Full Changelog**: compare", "## [1.0.0] - 2026-01-01", "- old")
	})

	t.Run("hand-written Unreleased entries move into the section", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n### Added\n\n- By hand\n\n## [1.0.0] - 2026-01-01\n", "",
			map[string]string{"v1.0.1": notes})
		r.release(t, "v1.0.0", "v1.0.1")

		if _, out, err := r.run(t, writeChangelogStep, "v1.0.1"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		got := r.changelog(t)
		inOrder(t, got, "## [Unreleased]", "## [1.0.1]", "- A fix", "Written by hand before this release:", "### Added", "- By hand", "## [1.0.0]")
		if strings.Count(got, "- By hand") != 1 {
			t.Fatalf("the hand-written entry is not there exactly once:\n%s", got)
		}
	})

	t.Run("a second release starts from an empty Unreleased", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n", "",
			map[string]string{"v1.0.1": notes, "v1.0.2": notes})
		r.release(t, "v1.0.0", "v1.0.1")
		if _, out, err := r.run(t, writeChangelogStep, "v1.0.1"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		mustGit(t, r.dir, "commit", "-q", "-am", "docs(changelog): v1.0.1")
		mustGit(t, r.dir, "push", "-q", "origin", "HEAD:main")
		mustGit(t, r.dir, "switch", "-q", "main")
		mustGit(t, r.dir, "pull", "-q", "origin", "main")
		r.release(t, "v1.0.2")

		if _, out, err := r.run(t, writeChangelogStep, "v1.0.2"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		got := r.changelog(t)
		inOrder(t, got, "## [Unreleased]", marker, "## [1.0.2]", "## [1.0.1]", "## [1.0.0]")
		if strings.Count(got, marker) != 1 || strings.Contains(got, "Written by hand") {
			t.Fatalf("the marker came back as a hand-written entry:\n%s", got)
		}
	})

	t.Run("an open pull request of an earlier release is replaced without losing its section", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n", "7",
			map[string]string{"v1.0.1": "- one\n", "v1.0.2": "- two\n"})
		r.release(t, "v1.0.0", "v1.0.1", "v1.0.2")

		if _, out, err := r.run(t, writeChangelogStep, "v1.0.2"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		if calls := r.ghCalls(t); !strings.Contains(calls, "pr close 7 --delete-branch") {
			t.Fatalf("the open pull request was not closed:\n%s", calls)
		}
		inOrder(t, r.changelog(t), "## [1.0.2]", "- two", "## [1.0.1]", "- one", "## [1.0.0]")
	})

	t.Run("an old pull request behind hundreds of open ones is found", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n", "7",
			map[string]string{"v1.0.1": "- one\n"})
		r.release(t, "v1.0.0", "v1.0.1")

		if _, out, err := r.run(t, writeChangelogStep, "v1.0.1", "GH_OPEN_POSITION=700"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		if calls := r.ghCalls(t); !strings.Contains(calls, "pr close 7 --delete-branch") {
			t.Fatalf("the open pull request was not closed:\n%s", calls)
		}
	})

	t.Run("mawk, the awk of a stock Ubuntu, writes the same file", func(t *testing.T) {
		mawk, err := exec.LookPath("mawk")
		if err != nil {
			t.Skip("no mawk on PATH")
		}
		changelog := changelogHead + "## [Unreleased]\n\n### Added\n\n- By hand\n\n## [1.0.0] - 2026-01-01\n\n" +
			"[Unreleased]: https://github.com/giantswarm/x/compare/v1.0.0...HEAD\n[1.0.0]: https://github.com/giantswarm/x/releases/tag/v1.0.0\n"
		written := map[string]string{}
		for _, awk := range []string{"awk", mawk} {
			r := newChangelogRepo(t, changelog, "", map[string]string{"v1.0.1": notes})
			r.release(t, "v1.0.0", "v1.0.1")
			if awk == mawk {
				if err := os.Symlink(mawk, filepath.Join(r.bin, "awk")); err != nil {
					t.Fatalf("link awk: %v", err)
				}
			}
			if _, out, err := r.run(t, writeChangelogStep, "v1.0.1"); err != nil {
				t.Fatalf("step failed under %s: %v\n%s", awk, err, out)
			}
			written[awk] = r.changelog(t)
		}
		if written["awk"] != written[mawk] {
			t.Fatalf("awk wrote:\n%s\nmawk wrote:\n%s", written["awk"], written[mawk])
		}
		inOrder(t, written[mawk], "## [Unreleased]", marker, "## [1.0.1]", "- A fix", "- By hand", "## [1.0.0]", "[Unreleased]: https://github.com/giantswarm/x/compare/v1.0.1...HEAD")
	})

	t.Run("a candidate tag on the branch is no section", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n", "",
			map[string]string{"v1.1.0": notes})
		r.release(t, "v1.0.0", "v1.1.0-rc.1", "v1.1.0-rc.2", "v1.1.0")

		if _, out, err := r.run(t, writeChangelogStep, "v1.1.0"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		got := r.changelog(t)
		inOrder(t, got, "## [1.1.0] - 2026-11-04", "## [1.0.0]")
		if strings.Contains(got, "rc.") {
			t.Fatalf("a candidate reached the file:\n%s", got)
		}
	})

	t.Run("a heading with the v prefix counts as named", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n## [v1.0.1] - 2026-01-01\n", "", nil)
		r.release(t, "v1.0.1")

		got, out, err := r.run(t, writeChangelogStep, "v1.0.1")
		if err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		if got["changed"] != "" {
			t.Fatalf("changed = %q, want nothing: a re-run writes nothing\n%s", got["changed"], out)
		}
	})

	t.Run("a tag without a GitHub release is left out", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n", "",
			map[string]string{"v1.0.2": "- two\n"})
		r.release(t, "v1.0.0", "v1.0.1", "v1.0.2")

		_, out, err := r.run(t, writeChangelogStep, "v1.0.2")
		if err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		if !strings.Contains(out, "v1.0.1 has no GitHub release") {
			t.Fatalf("no warning for the tag:\n%s", out)
		}
		got := r.changelog(t)
		inOrder(t, got, "## [1.0.2]", "## [1.0.0]")
		if strings.Contains(got, "## [1.0.1]") {
			t.Fatalf("a section without notes:\n%s", got)
		}
	})

	t.Run("a file without Unreleased gets the heading", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [1.0.0] - 2026-01-01\n", "", map[string]string{"v1.0.1": notes})
		r.release(t, "v1.0.0", "v1.0.1")

		if _, out, err := r.run(t, writeChangelogStep, "v1.0.1"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		inOrder(t, r.changelog(t), "# Changelog", "## [Unreleased]", "## [1.0.1]", "## [1.0.0]")
	})

	t.Run("a file holding nothing but Unreleased", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n- by hand\n", "", map[string]string{"v0.1.0": notes})
		r.release(t, "v0.1.0")

		if _, out, err := r.run(t, writeChangelogStep, "v0.1.0"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		inOrder(t, r.changelog(t), "## [Unreleased]", "## [0.1.0]", "- A fix", "- by hand")
	})

	t.Run("the link lines at the end of the file move on", func(t *testing.T) {
		r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n\n"+
			"[Unreleased]: https://github.com/giantswarm/x/compare/v1.0.0...HEAD\n[1.0.0]: https://github.com/giantswarm/x/releases/tag/v1.0.0\n", "",
			map[string]string{"v1.0.1": notes})
		r.release(t, "v1.0.0", "v1.0.1")

		if _, out, err := r.run(t, writeChangelogStep, "v1.0.1"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		inOrder(t, r.changelog(t),
			"[Unreleased]: https://github.com/giantswarm/x/compare/v1.0.1...HEAD\n",
			"[1.0.1]: https://github.com/giantswarm/x/compare/v1.0.0...v1.0.1\n",
			"[1.0.0]: https://github.com/giantswarm/x/releases/tag/v1.0.0\n")
	})

	t.Run("a repository without the file gets none", func(t *testing.T) {
		r := newChangelogRepo(t, "", "", map[string]string{"v1.0.1": notes})
		r.release(t, "v1.0.1")

		got, out, err := r.run(t, writeChangelogStep, "v1.0.1")
		if err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		if got["changed"] != "" {
			t.Fatalf("changed = %q, want nothing\n%s", got["changed"], out)
		}
		if _, err := os.Stat(filepath.Join(r.dir, "CHANGELOG.md")); !os.IsNotExist(err) {
			t.Fatalf("CHANGELOG.md was created: %v", err)
		}
	})
}

// Test_AutoReleaseChangelogFromGitCliff runs the acceptance of
// https://github.com/giantswarm/devctl/issues/2586 through the real git-cliff:
// two merged pull requests that never touched CHANGELOG.md are both listed
// under their group, from the notes the GitHub Release carries.
func Test_AutoReleaseChangelogFromGitCliff(t *testing.T) {
	requireGitCliff(t)

	dir := repo(t, "v1.0.0", "feat: add one (#1)", "fix: repair two (#2)")
	cliffTomlIn(t, dir)
	cmd := exec.CommandContext(t.Context(), "git-cliff", "--unreleased", "--bump", "--strip", "all")
	cmd.Dir = dir
	notes, err := cmd.Output()
	if err != nil {
		t.Fatalf("git-cliff: %v", err)
	}

	r := newChangelogRepo(t, changelogHead+"## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n", "",
		map[string]string{"v1.1.0": string(notes)})
	r.release(t, "v1.0.0", "v1.1.0")
	if _, out, err := r.run(t, writeChangelogStep, "v1.1.0"); err != nil {
		t.Fatalf("step failed: %v\n%s", err, out)
	}
	inOrder(t, r.changelog(t), "## [1.1.0]", "### Added", "- Add one", "### Fixed", "- Repair two", "## [1.0.0]")
}

// Test_AutoReleaseChangelogCommitReleasesNothing pins the end of the loop: the
// squash commit of the changelog pull request lands on the release branch and
// triggers the workflow, and git-cliff must see nothing to release in it.
func Test_AutoReleaseChangelogCommitReleasesNothing(t *testing.T) {
	requireGitCliff(t)

	dir := repo(t, "fix: repair", "v1.0.1", "docs(changelog): v1.0.1 (#3)")
	got := cliffBump(t, dir)
	if got.Version != "v1.0.1" || len(got.Commits) != 0 {
		t.Fatalf("git-cliff releases %s with %v, want v1.0.1 and no commits", got.Version, got.subjects())
	}
}

func Test_AutoReleaseChangelogPullRequest(t *testing.T) {
	changelog := changelogHead + "## [Unreleased]\n"

	t.Run("pushes the file alone and arms auto-merge", func(t *testing.T) {
		r := newChangelogRepo(t, changelog, "", nil)
		writeChangelog(t, r.dir, changelog+"\n## [1.0.1] - 2026-11-04\n")

		if _, out, err := r.run(t, openChangelogStep, "v1.0.1"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		head := "auto-release/changelog-v1.0.1"
		files := mustGit(t, r.origin, "diff", "--name-only", "main", head)
		if strings.TrimSpace(files) != "CHANGELOG.md" {
			t.Fatalf("the branch changes %q, want CHANGELOG.md alone", files)
		}
		subject := strings.TrimSpace(mustGit(t, r.origin, "log", "-1", "--format=%s", head))
		if subject != "docs(changelog): v1.0.1" {
			t.Fatalf("subject %q", subject)
		}
		author := mustGit(t, r.origin, "log", "-1", "--format=%an", head)
		if !strings.Contains(author, "giantswarm-release[bot]") {
			t.Fatalf("author %q", author)
		}
		calls := r.ghCalls(t)
		inOrder(t, calls, "pr create --base main --head "+head+" --title docs(changelog): v1.0.1", "pr merge --auto --squash https://github.com/giantswarm/x/pull/9")
	})

	t.Run("a branch a re-run finds is replaced, not forced", func(t *testing.T) {
		r := newChangelogRepo(t, changelog, "", nil)
		head := "auto-release/changelog-v1.0.1"
		mustGit(t, r.dir, "push", "-q", "origin", "main:"+head)
		mustGit(t, r.dir, "commit", "-q", "--allow-empty", "-m", "chore: moved on")
		mustGit(t, r.dir, "push", "-q", "origin", "main")
		writeChangelog(t, r.dir, changelog+"\n## [1.0.1] - 2026-11-04\n")

		if _, out, err := r.run(t, openChangelogStep, "v1.0.1"); err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		mustGit(t, r.origin, "merge-base", "--is-ancestor", "main", head)
	})

	t.Run("auto-merge refused is a warning", func(t *testing.T) {
		r := newChangelogRepo(t, changelog, "", nil)
		writeChangelog(t, r.dir, changelog+"\n## [1.0.1] - 2026-11-04\n")

		_, out, err := r.run(t, openChangelogStep, "v1.0.1", "GH_MERGE_EXIT=1")
		if err != nil {
			t.Fatalf("step failed: %v\n%s", err, out)
		}
		if !strings.Contains(out, "::warning::Could not enable auto-merge") {
			t.Fatalf("no warning:\n%s", out)
		}
	})
}

// Test_AutoReleaseChangelogWiring pins what the scripts cannot show: when the
// job runs, and that it can never cost a release its green run.
func Test_AutoReleaseChangelogWiring(t *testing.T) {
	wf := renderedChangelogWorkflow(t, gen.FlavourApp)

	job, ok := wf.Jobs["changelog"]
	if !ok {
		t.Fatal("no changelog job")
	}
	if job.Needs != "tag" {
		t.Fatalf("needs %q", job.Needs)
	}
	if !job.ContinueOnError {
		t.Fatal("a failed changelog job would fail the release's run")
	}
	if !strings.Contains(job.If, "needs.tag.outputs.stable != ''") {
		t.Fatalf("if %q", job.If)
	}

	// A candidate writes nothing, and so leaves nothing to cut.
	stable := wf.Jobs["tag"].Outputs["stable"]
	for _, want := range []string{"steps.release.outcome == 'success'", "steps.decide.outputs.prerelease == 'false'", "steps.decide.outputs.tag"} {
		if !strings.Contains(stable, want) {
			t.Fatalf("the stable output %q does not read %q", stable, want)
		}
	}

	t.Run("a fork line's changelog is upstream's", func(t *testing.T) {
		fork := renderedChangelogWorkflow(t, gen.FlavourFork)
		if _, ok := fork.Jobs["changelog"]; ok {
			t.Fatal("a fork line has a changelog job")
		}
		if _, ok := fork.Jobs["tag"].Outputs["stable"]; ok {
			t.Fatal("a fork line has the output of a job it does not have")
		}
	})
}
