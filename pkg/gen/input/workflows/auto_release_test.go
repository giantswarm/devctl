package workflows

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// autoReleaseStep is one step of the tag job as the rendered workflow declares
// it.
type autoReleaseStep struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	If   string `yaml:"if"`
	Run  string `yaml:"run"`
}

// tagJobSteps returns the steps of the auto-release tag job from the rendered
// workflow, so the tests below exercise the scripts that actually ship rather
// than a copy of them. The rendered workflow comes back with them, for the
// failure messages: a step looked up by id or name and not found says nothing
// on its own about what the template does declare.
func tagJobSteps(t *testing.T) ([]autoReleaseStep, string) {
	t.Helper()

	return tagJobStepsFor(t, gen.FlavourApp)
}

// tagJobStepsFor is tagJobSteps for a repository of the given flavours.
func tagJobStepsFor(t *testing.T, flavours ...gen.Flavour) ([]autoReleaseStep, string) {
	t.Helper()

	rendered := renderInput(t, newWorkflows(t, flavours...).AutoRelease())

	var wf struct {
		Jobs map[string]struct {
			Steps []autoReleaseStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, rendered)
	}

	return wf.Jobs["tag"].Steps, rendered
}

// tagJobStep returns the tag-job step with the given id.
func tagJobStep(t *testing.T, id string) autoReleaseStep {
	t.Helper()

	steps, rendered := tagJobSteps(t)
	for _, s := range steps {
		if s.ID == id {
			return s
		}
	}

	t.Fatalf("no step with id %q in the tag job:\n%s", id, rendered)
	return autoReleaseStep{}
}

// decideScript extracts the shell of the "Decide whether to tag" step.
func decideScript(t *testing.T) string {
	t.Helper()

	return tagJobStep(t, "decide").Run
}

// gitIn runs git in dir and returns its combined output, leaving the error for
// the caller. Use it where a non-zero exit is a legitimate answer.
//
// The user's and the system's git config are not read: a developer's
// commit.gpgsign or tag.gpgsign would make every commit here ask gpg for a key
// the test identity does not have, and a core.hooksPath or commit.template
// would change what the history looks like.
func gitIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) // #nosec G204 -- fixed binary, args are built by this test, test-only
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	out, err := cmd.CombinedOutput()

	return string(out), err
}

// mustGit runs git in dir and fails the test on a non-zero exit.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	out, err := gitIn(t, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}

	return out
}

// repo builds a git history for one test case. A "vX.Y.Z" entry tags the
// current HEAD, anything else becomes an empty commit with that subject. An
// entry split by a blank line becomes a subject plus a commit body, which is
// how a case declares a `BREAKING CHANGE:` footer.
func repo(t *testing.T, history ...string) string {
	t.Helper()

	dir := t.TempDir()

	mustGit(t, dir, "init", "--initial-branch=main")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "chore: inception")
	for _, entry := range history {
		if strings.HasPrefix(entry, "v") {
			mustGit(t, dir, "tag", entry)
			continue
		}
		subject, body, hasBody := strings.Cut(entry, "\n\n")
		args := []string{"commit", "--allow-empty", "-m", subject}
		if hasBody {
			args = append(args, "-m", body)
		}
		mustGit(t, dir, args...)
	}

	return dir
}

// cliffSkippedTypes are the commit_parsers in cliff.toml that carry
// `skip = true`. git-cliff leaves them out of both the release notes and the
// bump, so they are not releasable on their own.
var cliffSkippedTypes = []string{"docs", "style"}

// countedSubject matches a conventional subject and captures its type. The
// optional -rc mirrors cliff.toml's commit_preprocessor, which normalises
// feat-rc/fix-rc to feat/fix before git-cliff parses the type.
var countedSubject = regexp.MustCompile(`^([a-z]+)(?:-rc)?(?:\([^)]*\))?!?: `)

// cliffContext writes the cliff-context.json that the decide step reads. The
// test environment has no git-cliff, so this stands in for it: the file holds
// the commits git-cliff would count for the bumped release, which is every
// commit since the last stable tag that is conventional
// (`filter_unconventional`) and not marked skip in cliff.toml.
func cliffContext(t *testing.T, dir string) {
	t.Helper()

	args := []string{"log", "-z", "--format=%H %s"}
	if last, err := gitIn(t, dir, "describe", "--tags", "--abbrev=0", "--match=v*.*.*", "--exclude=*-*", "--exclude=*+*"); err == nil {
		args = append(args, strings.TrimSpace(last)+"..HEAD")
	}

	commits := []map[string]string{}
	for _, record := range strings.Split(mustGit(t, dir, args...), "\x00") {
		id, subject, ok := strings.Cut(record, " ")
		if !ok {
			continue
		}
		match := countedSubject.FindStringSubmatch(subject)
		if match == nil || slices.Contains(cliffSkippedTypes, match[1]) {
			continue
		}
		commits = append(commits, map[string]string{"id": id})
	}

	raw, err := json.Marshal([]map[string]any{{"commits": commits}})
	if err != nil {
		t.Fatalf("marshal cliff context: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cliff-context.json"), raw, 0o600); err != nil {
		t.Fatalf("write cliff context: %v", err)
	}
}

// decide runs the extracted step in dir and returns its GITHUB_OUTPUT as a map.
func decide(t *testing.T, script, dir, next, want string) map[string]string {
	t.Helper()

	got, _ := decideWithLog(t, script, dir, next, want)

	return got
}

// decideWithLog runs the extracted step in dir and returns its GITHUB_OUTPUT
// as a map together with what the step printed.
func decideWithLog(t *testing.T, script, dir, next, want string) (map[string]string, string) {
	t.Helper()

	cliffContext(t, dir)

	got, out, err := runStep(t, script, dir, "NEXT="+next, "WANT="+want, "CLIFF_CONTEXT=cliff-context.json")
	if err != nil {
		t.Fatalf("decide step failed: %v\n%s", err, out)
	}

	return got, out
}

// runStep runs a tag-job step's shell in dir with env on top of the runner's
// GITHUB_* variables, and returns its GITHUB_OUTPUT as a map, what it printed
// and its exit error.
func runStep(t *testing.T, script, dir string, env ...string) (map[string]string, string, error) {
	t.Helper()

	outPath := filepath.Join(t.TempDir(), "github-output")
	if err := os.WriteFile(outPath, nil, 0o600); err != nil {
		t.Fatalf("seed GITHUB_OUTPUT: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "bash", "-c", script) // #nosec G204 -- the script is a literal in this test, test-only
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GITHUB_REF_NAME=main",
		"GITHUB_OUTPUT="+outPath,
		"GITHUB_STEP_SUMMARY="+filepath.Join(t.TempDir(), "step-summary"),
	)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()

	raw, readErr := os.ReadFile(outPath) // #nosec G304 -- path built from t.TempDir
	if readErr != nil {
		t.Fatalf("read GITHUB_OUTPUT: %v", readErr)
	}

	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			got[key] = value
		}
	}

	return got, string(out), err
}

// Test_AutoReleaseDecideWarnsOnUnconventionalSubjects pins that a commit
// git-cliff cannot parse is named in the run as a workflow warning. Such a
// commit neither bumps the version nor appears in any release's notes; a
// one-commit pull request lands one whenever the repository names the
// squash commit after the commit rather than the pull request's title. The
// warning is the only trace the run leaves, so it has to be there, and the
// conventional subjects must not trigger it.
func Test_AutoReleaseDecideWarnsOnUnconventionalSubjects(t *testing.T) {
	script := decideScript(t)

	t.Run("an unconventional subject is warned about and releases nothing", func(t *testing.T) {
		dir := repo(t, "v1.2.9", "portal Component: no lists on the hub (#110)")
		got, log := decideWithLog(t, script, dir, "v1.2.9", "auto")

		if got["tag"] != "" {
			t.Errorf("tag = %q, want none", got["tag"])
		}
		if !strings.Contains(log, `::warning title=Unconventional commit subject::"portal Component: no lists on the hub (#110)"`) {
			t.Errorf("no warning naming the subject in the step's output:\n%s", log)
		}
		if !strings.Contains(log, "1 unconventional") {
			t.Errorf("the summary line does not count the commit:\n%s", log)
		}
	})

	t.Run("a conventional subject of any type is not warned about", func(t *testing.T) {
		dir := repo(t, "v1.2.9", "chore(deps): bump y", "docs: fix typo", "feat-rc(auth): add x", "refactor!: drop y", "fix: last thing")
		_, log := decideWithLog(t, script, dir, "v2.0.0", "auto")

		if strings.Contains(log, "::warning") {
			t.Errorf("a warning for a conventional subject:\n%s", log)
		}
		if !strings.Contains(log, "0 unconventional") {
			t.Errorf("the summary line does not read zero:\n%s", log)
		}
	})
}

// Test_AutoReleaseDecide pins the release rule: every releasable push cuts
// the next rc.N for the version git-cliff computed (`next`), and a push with
// nothing releasable since the last stable release or the last candidate
// cuts nothing.
func Test_AutoReleaseDecide(t *testing.T) {
	script := decideScript(t)

	testCases := []struct {
		name      string
		history   []string
		next      string
		expectTag string
	}{
		{
			name:      "a feat opens a cycle",
			history:   []string{"v1.2.9", "feat: add x"},
			next:      "v1.3.0",
			expectTag: "v1.3.0-rc.1",
		},
		{
			name:      "a fix opens a cycle",
			history:   []string{"v1.2.9", "fix: x"},
			next:      "v1.2.10",
			expectTag: "v1.2.10-rc.1",
		},
		{
			name:      "chore(deps) during a cycle cuts the next candidate",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "chore(deps): bump y"},
			next:      "v1.3.0",
			expectTag: "v1.3.0-rc.2",
		},
		{
			// `docs` is skipped by cliff.toml, so there is nothing new to put
			// in a candidate.
			name:      "a docs-only push during a cycle releases nothing",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "docs: fix typo"},
			next:      "v1.3.0",
			expectTag: "",
		},
		{
			name:      "a non-conventional push during a cycle releases nothing",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "Merge pull request #12 from foo/bar"},
			next:      "v1.3.0",
			expectTag: "",
		},
		{
			name:      "a breaking change moves the target and restarts at rc.1",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "refactor!: drop y"},
			next:      "v2.0.0",
			expectTag: "v2.0.0-rc.1",
		},
		{
			name:      "rc.9 is followed by rc.10, not rc.2",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.9", "fix: tweak"},
			next:      "v1.3.0",
			expectTag: "v1.3.0-rc.10",
		},
		{
			name:      "a leftover -rc type still cuts a candidate",
			history:   []string{"v1.2.9", "feat-rc: add x"},
			next:      "v1.3.0",
			expectTag: "v1.3.0-rc.1",
		},
		{
			name:      "inception, with no tag at all",
			history:   []string{"feat: add x"},
			next:      "v0.1.0",
			expectTag: "v0.1.0-rc.1",
		},
		{
			name:      "no releasable commits, so no tag",
			history:   []string{"v1.3.0"},
			next:      "v1.3.0",
			expectTag: "",
		},
		{
			name:      "after a stable release the next fix opens a new cycle",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "v1.3.0", "fix: y"},
			next:      "v1.3.1",
			expectTag: "v1.3.1-rc.1",
		},
		{
			// On release-2.x the reachable baseline is v2.3.5, not the higher
			// v3.x tags on main, which git-cliff has already accounted for in
			// `next`. The step must agree, or it reports "nothing to release".
			name:      "a backport branch bumps from its own reachable baseline",
			history:   []string{"v2.3.5", "fix: backport"},
			next:      "v2.3.6",
			expectTag: "v2.3.6-rc.1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t, tc.history...)
			got := decide(t, script, dir, tc.next, "auto")

			if got["tag"] != tc.expectTag {
				t.Errorf("tag = %q, want %q", got["tag"], tc.expectTag)
			}
			if tc.expectTag == "" {
				return
			}
			if got["prerelease"] != "true" {
				t.Errorf("prerelease = %q, want true", got["prerelease"])
			}
			if head := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD")); got["target"] != head {
				t.Errorf("target = %q, want HEAD %q", got["target"], head)
			}
		})
	}
}

// ghStub puts a gh on PATH that answers the promote step's two questions:
// `gh release view` prints release ("true" or "false" for isPrerelease; empty
// fails as for a missing release), `gh api .../status` prints status ("<state>
// <total_count>"; empty fails as for a token without statuses: read). It
// returns the PATH entry for runStep.
func ghStub(t *testing.T, release, status string) string {
	t.Helper()

	dir := t.TempDir()
	script := "#!/usr/bin/env bash\n" +
		"case \"$1 $2\" in\n" +
		"  'release view') [ -n '" + release + "' ] || exit 1; echo '" + release + "' ;;\n" +
		"  api*) [ -n '" + status + "' ] || exit 1; echo '" + status + "' ;;\n" +
		"  *) echo \"unexpected gh $*\" >&2; exit 2 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700); err != nil { // #nosec G306 -- test-only executable stub
		t.Fatalf("write gh stub: %v", err)
	}

	return "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// builtCandidate is the gh stub of a candidate whose pre-release exists and
// whose pipelines passed.
func builtCandidate(t *testing.T) string {
	t.Helper()

	return ghStub(t, "true", "success 2")
}

// promoteScript extracts the shell of the step that checks out the candidate
// a stable release promotes.
func promoteScript(t *testing.T) string {
	t.Helper()

	return tagJobStep(t, "promote").Run
}

// Test_AutoReleasePromote pins what `release-type: stable` releases: the
// latest candidate since the last stable release, on that candidate's commit,
// whatever merged after it. The promote step checks the candidate out and the
// decide step, run where it left HEAD, tags the version on that commit.
func Test_AutoReleasePromote(t *testing.T) {
	promote := promoteScript(t)
	decideStep := decideScript(t)

	testCases := []struct {
		name      string
		history   []string
		next      string
		expectRC  string
		expectTag string
	}{
		{
			name:      "the latest candidate is promoted",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "fix: y", "v1.3.0-rc.2"},
			next:      "v1.3.0",
			expectRC:  "v1.3.0-rc.2",
			expectTag: "v1.3.0",
		},
		{
			name:      "commits after the candidate stay out of the release",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "docs: tidy", "fix: late"},
			next:      "v1.3.0",
			expectRC:  "v1.3.0-rc.1",
			expectTag: "v1.3.0",
		},
		{
			name:      "rc.10 is later than rc.9",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.9", "fix: y", "v1.3.0-rc.10"},
			next:      "v1.3.0",
			expectRC:  "v1.3.0-rc.10",
			expectTag: "v1.3.0",
		},
		{
			name:      "a moved target is promoted at its own version",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "refactor!: drop y", "v2.0.0-rc.1"},
			next:      "v2.0.0",
			expectRC:  "v2.0.0-rc.1",
			expectTag: "v2.0.0",
		},
		{
			name:      "candidates of an earlier stable release do not count",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "v1.3.0", "fix: y", "v1.3.1-rc.1"},
			next:      "v1.3.1",
			expectRC:  "v1.3.1-rc.1",
			expectTag: "v1.3.1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t, tc.history...)

			promoted, log, err := runStep(t, promote, dir, builtCandidate(t), "GH_REPO=giantswarm/example")
			if err != nil {
				t.Fatalf("promote step failed: %v\n%s", err, log)
			}
			if promoted["rc"] != tc.expectRC {
				t.Fatalf("rc = %q, want %q", promoted["rc"], tc.expectRC)
			}

			cliffContext(t, dir)
			got, log, err := runStep(t, decideStep, dir,
				"NEXT="+tc.next, "WANT=stable", "PROMOTE="+promoted["rc"], "CLIFF_CONTEXT=cliff-context.json")
			if err != nil {
				t.Fatalf("decide step failed: %v\n%s", err, log)
			}

			if got["tag"] != tc.expectTag {
				t.Errorf("tag = %q, want %q", got["tag"], tc.expectTag)
			}
			if got["prerelease"] != "false" {
				t.Errorf("prerelease = %q, want false", got["prerelease"])
			}
			rcCommit := strings.TrimSpace(mustGit(t, dir, "rev-parse", tc.expectRC+"^{commit}"))
			if got["target"] != rcCommit {
				t.Errorf("target = %q, want the commit of %s %q", got["target"], tc.expectRC, rcCommit)
			}
		})
	}
}

// Test_AutoReleasePromoteRefuses pins the refusals of a stable release: no
// candidate to promote, a candidate without its GitHub pre-release or with a
// failed or running pipeline, and a candidate whose commits release a
// different version than its tag names.
func Test_AutoReleasePromoteRefuses(t *testing.T) {
	t.Run("no candidate since the last stable release", func(t *testing.T) {
		for _, history := range [][]string{
			{"v1.2.9", "fix: x"},
			{"v1.2.9", "fix: x", "v1.2.10-rc.1", "v1.2.10", "fix: y"},
		} {
			_, log, err := runStep(t, promoteScript(t), repo(t, history...), builtCandidate(t), "GH_REPO=giantswarm/example")
			if err == nil {
				t.Errorf("history %q: promote step succeeded, want a refusal:\n%s", history, log)
			}
			if !strings.Contains(log, "::error title=No release candidate to promote::") {
				t.Errorf("history %q: no error annotation:\n%s", history, log)
			}
		}
	})

	for _, tc := range []struct {
		name, release, status, annotation string
	}{
		{"the candidate has no GitHub release", "", "success 2", "Candidate not released"},
		{"the candidate's release is not a pre-release", "false", "success 2", "Candidate not a pre-release"},
		{"a pipeline of the candidate failed", "true", "failure 2", "Candidate not built"},
		{"a pipeline of the candidate is still running", "true", "pending 1", "Candidate not built"},
		{"the candidate's statuses cannot be read", "true", "", "Candidate status unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t, "v1.2.9", "fix: x", "v1.2.10-rc.1")
			_, log, err := runStep(t, promoteScript(t), dir, ghStub(t, tc.release, tc.status), "GH_REPO=giantswarm/example")
			if err == nil {
				t.Errorf("promote step succeeded, want a refusal:\n%s", log)
			}
			if !strings.Contains(log, "::error title="+tc.annotation+"::") {
				t.Errorf("no %q error annotation:\n%s", tc.annotation, log)
			}
		})
	}

	t.Run("a candidate no pipeline reports on is promoted", func(t *testing.T) {
		dir := repo(t, "v1.2.9", "fix: x", "v1.2.10-rc.1")
		got, log, err := runStep(t, promoteScript(t), dir, ghStub(t, "true", "pending 0"), "GH_REPO=giantswarm/example")
		if err != nil || got["rc"] != "v1.2.10-rc.1" {
			t.Errorf("rc = %q, err = %v, want v1.2.10-rc.1:\n%s", got["rc"], err, log)
		}
	})

	t.Run("the candidate's commits release another version", func(t *testing.T) {
		dir := repo(t, "v1.2.9", "fix: x", "v1.2.10-rc.1")
		cliffContext(t, dir)

		_, log, err := runStep(t, decideScript(t), dir,
			"NEXT=v1.3.0", "WANT=stable", "PROMOTE=v1.2.10-rc.1", "CLIFF_CONTEXT=cliff-context.json")
		if err == nil {
			t.Errorf("decide step succeeded, want a refusal:\n%s", log)
		}
		if !strings.Contains(log, "::error title=Candidate does not match::") {
			t.Errorf("no error annotation:\n%s", log)
		}
	})
}

// Test_AutoReleaseDecideIgnoresUnmergedCandidates pins that the rc counter is
// scoped to reachable tags. The describe baseline is scoped for the backport
// case; a candidate for the same target cut on another branch must not shift
// the numbering on this one either.
func Test_AutoReleaseDecideIgnoresUnmergedCandidates(t *testing.T) {
	script := decideScript(t)

	dir := repo(t, "v1.2.9", "feat: add x")

	// A candidate cut on a side branch that never merged. cliff.toml keeps it
	// out of git-cliff's baseline through use_branch_tags, so `NEXT` below is
	// still v1.3.0; the step has to agree.
	mustGit(t, dir, "checkout", "-q", "-b", "side", "v1.2.9")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "feat: something else")
	mustGit(t, dir, "tag", "v1.3.0-rc.7")
	mustGit(t, dir, "checkout", "-q", "main")

	got := decide(t, script, dir, "v1.3.0", "auto")

	if got["tag"] != "v1.3.0-rc.1" {
		t.Errorf("tag = %q, want v1.3.0-rc.1: the unmerged v1.3.0-rc.7 must not count", got["tag"])
	}
}

// Test_AutoReleaseDescribeExcludesNonReleaseTags pins the --exclude flags.
// `--match='v*.*.*'` is a glob and matches v1.3.0-rc.1 and v1.2.4+build.1 as
// readily as v1.2.3. Either one left in returns a baseline cliff.toml's
// tag_pattern does not count as a release, the "nothing to release"
// comparison can never be true again, and the job tags on every push.
func Test_AutoReleaseDescribeExcludesNonReleaseTags(t *testing.T) {
	script := decideScript(t)

	for _, exclude := range []string{"--exclude='*-*'", "--exclude='*+*'"} {
		if !strings.Contains(script, exclude) {
			t.Errorf("decide step describe baseline is missing %s:\n%s", exclude, script)
		}
	}
}

// Test_AutoReleaseDecideIgnoresBuildMetadataTags runs the case `*+*` exists
// for. cliff.toml's tag_pattern does not count v1.2.3+build.1 as a release, so
// git-cliff reports v1.2.3 as the target of a push that carries nothing
// releasable. The step reaches the same baseline and cuts nothing; a describe
// that returns the build-metadata tag makes the comparison false and tags
// v1.2.3 a second time.
func Test_AutoReleaseDecideIgnoresBuildMetadataTags(t *testing.T) {
	dir := repo(t, "v1.2.3", "v1.2.3+build.1", "docs: tidy")

	got := decide(t, decideScript(t), dir, "v1.2.3", "auto")

	if got["tag"] != "" {
		t.Errorf("tag = %q, want none: v1.2.3+build.1 is not a release", got["tag"])
	}
}

// Test_AutoReleaseCliffNormalisesRcTypes pins the contract between the
// workflow and cliff.toml: the workflow reads the raw -rc subjects, so
// git-cliff must be handed the plain type. features_always_bump_minor keys off
// the type being exactly "feat", and an un-normalised feat-rc bumps patch.
func Test_AutoReleaseCliffNormalisesRcTypes(t *testing.T) {
	cliff := renderInput(t, newWorkflows(t, gen.FlavourApp).CliffToml())

	if !strings.Contains(cliff, `{ pattern = '^(feat|fix)-rc', replace = "${1}" }`) {
		t.Errorf("cliff.toml does not normalise feat-rc/fix-rc before parsing:\n%s", cliff)
	}
	if !strings.Contains(cliff, "features_always_bump_minor = true") {
		t.Error("cliff.toml no longer sets features_always_bump_minor; the normalisation above may be pointless")
	}
}

// Test_SemanticPullRequestTypes pins the accepted PR title types. The -rc
// types are gone: every releasable push cuts a release candidate, so a title
// has nothing to mark.
func Test_SemanticPullRequestTypes(t *testing.T) {
	got := renderInput(t, newWorkflows(t, gen.FlavourApp).SemanticPullRequest())

	var wf struct {
		Jobs map[string]struct {
			With map[string]string `yaml:"with"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
	}

	with := wf.Jobs["semantic-pull-request"].With

	if pattern, ok := with["header_pattern"]; ok {
		t.Errorf("header_pattern = %q, want the action's default parser", pattern)
	}

	// `types` replaces the action's default list rather than extending it, so
	// dropping one of these silently blocks that type on every repository.
	// `security` is not one of the action's defaults, so it needs the explicit
	// list to be accepted at all; cliff.toml maps it to the Security group.
	types := strings.Fields(with["types"])
	wantTypes := []string{"feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert", "security"}
	if !slices.Equal(types, wantTypes) {
		t.Errorf("types = %q, want %q", types, wantTypes)
	}
}

// notesEnv lays out a working directory for the notes step: the cliff context
// it reads, and a stub git-cliff on PATH that writes out the version it is
// handed. The stub is what lets the assertion read the version the real render
// would put in the compare link.
func notesEnv(t *testing.T, context string) (dir, bin string) {
	t.Helper()

	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cliff-context.json"), []byte(context), 0o600); err != nil {
		t.Fatalf("write cliff context: %v", err)
	}

	bin = filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o750); err != nil {
		t.Fatalf("make stub bin dir: %v", err)
	}

	stub := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    --from-context) ctx=$2; shift 2 ;;\n" +
		"    --output) out=$2; shift 2 ;;\n" +
		"    *) shift ;;\n" +
		"  esac\n" +
		"done\n" +
		"sed -n 's/.*\"version\": *\"\\([^\"]*\\)\".*/\\1/p' \"$ctx\" | head -1 > \"$out\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git-cliff"), []byte(stub), 0o700); err != nil { // #nosec G306 -- the stub has to be executable
		t.Fatalf("write git-cliff stub: %v", err)
	}

	return dir, bin
}

// runNotes runs the extracted notes step and returns its combined output.
func runNotes(t *testing.T, script, dir, bin, tag string) ([]byte, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "bash", "-c", script) // #nosec G204 -- the script is a literal in this test, test-only
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"TAG="+tag,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	return cmd.CombinedOutput()
}

// Test_AutoReleaseNotesUseTheTaggedVersion pins the version the notes carry.
// cliff.toml renders `{{ version }}` from the context into the "Full Changelog"
// compare link, so the context has to name the tag the workflow cuts, not the
// stable target it bumped to. The stub stands in for git-cliff, so what is
// pinned is the version the render is handed, not the link cliff.toml builds
// out of it.
func Test_AutoReleaseNotesUseTheTaggedVersion(t *testing.T) {
	notes := tagJobStep(t, "notes")

	if notes.If != "steps.decide.outputs.tag != ''" {
		t.Errorf("notes step condition = %q, want it skipped when nothing is tagged", notes.If)
	}

	dir, bin := notesEnv(t, `[{"version":"v0.1.6","commits":[]}]`)

	if out, err := runNotes(t, notes.Run, dir, bin, "v0.1.6-rc.1"); err != nil {
		t.Fatalf("notes step failed: %v\n%s", err, out)
	}

	rendered, err := os.ReadFile(filepath.Join(dir, "release-notes.md")) // #nosec G304 -- path built from t.TempDir
	if err != nil {
		t.Fatalf("read release notes: %v", err)
	}

	if strings.TrimSpace(string(rendered)) != "v0.1.6-rc.1" {
		t.Errorf("rendered version = %q, want the tag the workflow cuts", strings.TrimSpace(string(rendered)))
	}
}

// Test_AutoReleaseNotesRejectAnEmptyContext pins the guard on the patch. jq
// builds `[{"version": $tag}]` out of an empty context, which renders empty
// notes onto a tag that is about to be created. The decide step reads the same
// field and skips the tag when it is empty, so the guard only catches a context
// that the two steps disagree about.
func Test_AutoReleaseNotesRejectAnEmptyContext(t *testing.T) {
	dir, bin := notesEnv(t, `[]`)

	out, err := runNotes(t, tagJobStep(t, "notes").Run, dir, bin, "v0.1.6-rc.1")
	if err == nil {
		t.Fatalf("notes step succeeded on an empty context:\n%s", out)
	}
	if !strings.Contains(string(out), "carries no version") {
		t.Errorf("notes step failed without naming the cause:\n%s", out)
	}
}

// Test_AutoReleaseRenderOrder pins where the render sits. The tag is only known
// after the decision, so a render above it can only name the stable target, and
// `gh release create` reads release-notes.md, so a render below the release
// fails the run on a missing file.
func Test_AutoReleaseRenderOrder(t *testing.T) {
	steps, rendered := tagJobSteps(t)

	decideAt, notesAt, releaseAt := -1, -1, -1
	for i, s := range steps {
		switch s.ID {
		case "decide":
			decideAt = i
		case "notes":
			notesAt = i
		case "release":
			releaseAt = i
		}
	}

	if decideAt < 0 || notesAt < 0 || releaseAt < 0 {
		t.Fatalf("tag job is missing decide (%d), notes (%d) or release (%d):\n%s",
			decideAt, notesAt, releaseAt, rendered)
	}
	if notesAt < decideAt {
		t.Errorf("notes step is at %d, before the decide step at %d", notesAt, decideAt)
	}
	if notesAt > releaseAt {
		t.Errorf("notes step is at %d, after the release step at %d", notesAt, releaseAt)
	}
}

// cliffRemoteSection matches the [remote.github] header and every line after
// it that does not open a new section.
var cliffRemoteSection = regexp.MustCompile(`(?m)^\[remote\.github\]\n(?:(?:[^\[\n].*)?\n)*`)

// requireGitCliff resolves the git-cliff the cases below run. A workstation
// without a working one skips; CI fails. `make test` installs the binary, so
// a CI run that cannot run it has lost every case that exercises the tag
// selection cliff.toml configures, and has to say so rather than report
// green. The probe runs the binary rather than looking it up: a build for the
// wrong libc is on PATH and executable, and fails only when a case calls it.
func requireGitCliff(t *testing.T) {
	t.Helper()

	err := exec.CommandContext(t.Context(), "git-cliff", "--version").Run()
	if err == nil {
		return
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("git-cliff does not run (`make test` installs it): %v", err)
	}

	t.Skipf("no usable git-cliff; run `make test` or put one on PATH: %v", err)
}

// cliffTomlIn renders cliff.toml into dir for a git-cliff run. The
// [remote.github] block is dropped: it drives the PR and author links in the
// notes through the GitHub API, which a test has no token and no network for,
// and git-cliff aborts on the failed lookup. Nothing in tag selection or the
// bump reads it.
func cliffTomlIn(t *testing.T, dir string) {
	t.Helper()

	rendered := renderInput(t, newWorkflows(t, gen.FlavourApp).CliffToml())

	stripped := cliffRemoteSection.ReplaceAllString(rendered, "")
	if stripped == rendered {
		t.Fatalf("rendered cliff.toml has no [remote.github] block:\n%s", rendered)
	}

	if err := os.WriteFile(filepath.Join(dir, "cliff.toml"), []byte(stripped), 0o600); err != nil {
		t.Fatalf("write cliff.toml: %v", err)
	}
}

// cliffRelease is the part of git-cliff's JSON context the tests below read.
type cliffRelease struct {
	Version string `json:"version"`
	Commits []struct {
		Message string `json:"message"`
	} `json:"commits"`
}

// cliffBump runs what the "Compute next version" step runs and returns the
// release git-cliff bumped to.
func cliffBump(t *testing.T, dir string) cliffRelease {
	t.Helper()

	cliffTomlIn(t, dir)

	cmd := exec.CommandContext(t.Context(), "git-cliff", "--unreleased", "--bump", "--context")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git-cliff: %v\n%s", err, out)
	}

	var releases []cliffRelease
	if err := json.Unmarshal(out, &releases); err != nil {
		t.Fatalf("git-cliff context is not valid JSON: %v\n%s", err, out)
	}
	if len(releases) != 1 {
		t.Fatalf("git-cliff returned %d releases, want 1:\n%s", len(releases), out)
	}

	return releases[0]
}

// subjects returns the first line of each commit of a release.
func (r cliffRelease) subjects() []string {
	out := make([]string, 0, len(r.Commits))
	for _, c := range r.Commits {
		subject, _, _ := strings.Cut(c.Message, "\n")
		out = append(out, subject)
	}

	return out
}

// Test_AutoReleaseCliffSpansTheWholeCandidateCycle pins what `--unreleased`
// means while a candidate cycle is open. A vX.Y.Z-rc.N tag sits on the branch
// head for most of a cycle; cliff.toml's tag_pattern keeps it from being a
// release, so the version and the notes cover every commit since the last
// stable tag rather than only the ones since the last candidate.
func Test_AutoReleaseCliffSpansTheWholeCandidateCycle(t *testing.T) {
	requireGitCliff(t)

	testCases := []struct {
		name           string
		history        []string
		expectVersion  string
		expectSubjects []string
	}{
		{
			// The stable release promoted from a cycle carries what every
			// candidate carried, and the minor the feat earned.
			name:           "a promoted candidate covers the whole cycle",
			history:        []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "fix: last thing", "v1.3.0-rc.2"},
			expectVersion:  "v1.3.0",
			expectSubjects: []string{"add x", "last thing"},
		},
		{
			// The second candidate of a cycle keeps the target the first one
			// established, instead of bumping a patch off the last stable.
			name:           "a later candidate keeps the target of the cycle",
			history:        []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "fix: tweak"},
			expectVersion:  "v1.3.0",
			expectSubjects: []string{"add x", "tweak"},
		},
		{
			// A stable run checks the candidate out, so git-cliff runs with
			// the candidate tag on HEAD.
			name:           "a candidate tag on HEAD still names the target",
			history:        []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1"},
			expectVersion:  "v1.3.0",
			expectSubjects: []string{"add x"},
		},
		{
			// use_branch_tags is what scopes the baseline to the branch, and
			// tag_pattern must not cost the 2.x line its own candidates.
			name:           "a maintenance branch cuts its own cycle",
			history:        []string{"v2.3.5", "feat: backport", "v2.4.0-rc.1", "fix: last thing"},
			expectVersion:  "v2.4.0",
			expectSubjects: []string{"backport", "last thing"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			release := cliffBump(t, repo(t, tc.history...))

			if release.Version != tc.expectVersion {
				t.Errorf("version = %q, want %q", release.Version, tc.expectVersion)
			}
			if got := release.subjects(); !slices.Equal(got, tc.expectSubjects) {
				t.Errorf("commits = %q, want %q", got, tc.expectSubjects)
			}
		})
	}
}

// Test_AutoReleaseCliffOnAPromotedCandidate pins that git-cliff, run on the
// detached candidate the promote step checks out, releases that candidate's
// version and commits and nothing merged after it.
func Test_AutoReleaseCliffOnAPromotedCandidate(t *testing.T) {
	requireGitCliff(t)

	dir := repo(t, "v1.2.9", "fix: x", "v1.2.10-rc.1", "feat: late")
	mustGit(t, dir, "checkout", "-q", "--detach", "v1.2.10-rc.1")

	release := cliffBump(t, dir)

	if release.Version != "v1.2.10" {
		t.Errorf("version = %q, want v1.2.10", release.Version)
	}
	if got := release.subjects(); !slices.Equal(got, []string{"x"}) {
		t.Errorf("commits = %q, want [x]", got)
	}
}

// Test_AutoReleaseCliffCountsOnlyStableTags pins the config key the behaviour
// above rests on. The decide step reaches the same set of tags through
// `git describe`, and compares `NEXT` against what it returns; a baseline one
// of the two does not recognise makes the step either tag on every push or
// never tag at all. Test_AutoReleaseDescribeExcludesNonReleaseTags pins the
// describe end of that pair.
func Test_AutoReleaseCliffCountsOnlyStableTags(t *testing.T) {
	cliff := renderInput(t, newWorkflows(t, gen.FlavourApp).CliffToml())

	if !strings.Contains(cliff, `tag_pattern = '^v[0-9]+\.[0-9]+\.[0-9]+$'`) {
		t.Errorf("cliff.toml does not restrict releases to stable v tags:\n%s", cliff)
	}
}

// tagJobStepNamed returns the tag-job step with the given name, for the steps
// the template declares without an id.
func tagJobStepNamed(t *testing.T, name string) autoReleaseStep {
	t.Helper()

	steps, rendered := tagJobSteps(t)
	for _, s := range steps {
		if s.Name == name {
			return s
		}
	}

	t.Fatalf("no step named %q in the tag job:\n%s", name, rendered)
	return autoReleaseStep{}
}

// curlStub is the curl the verify step's cases run: it answers a GET with the
// GET body and code it is handed and a POST with the POST body and code, in
// the body-newline-code shape the step's `-w '\n%{http_code}'` produces.
const curlStub = `#!/usr/bin/env bash
set -euo pipefail
method=GET
while [ $# -gt 0 ]; do
  case "$1" in
    -X) method=$2; shift 2 ;;
    *) shift ;;
  esac
done
if [ "$method" = POST ]; then
  printf '%s\n%s' "$STUB_POST_BODY" "$STUB_POST_CODE"
else
  printf '%s\n%s' "$STUB_GET_BODY" "$STUB_GET_CODE"
fi
`

// runVerify runs the extracted verify step against the curl stub and returns
// its combined output. The wait is zero so an empty list is final after one
// pass; the codes that end the wait at once (404, 401, 403) never reach it.
func runVerify(t *testing.T, token string, get, post [2]string) ([]byte, error) {
	t.Helper()

	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(curlStub), 0o700); err != nil { // #nosec G306 -- the stub has to be executable
		t.Fatalf("write curl stub: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "bash", "-c", tagJobStepNamed(t, "Verify CircleCI picked up the tag").Run) // #nosec G204 -- the script is the rendered template, test-only
	cmd.Env = append(os.Environ(),
		"TAG=v0.1.0",
		"GITHUB_REPOSITORY=example/widget",
		"CIRCLECI_API_TOKEN="+token,
		"CIRCLECI_PIPELINE_WAIT_SECONDS=0",
		"STUB_GET_BODY="+get[0], "STUB_GET_CODE="+get[1],
		"STUB_POST_BODY="+post[0], "STUB_POST_CODE="+post[1],
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	return cmd.CombinedOutput()
}

// Test_AutoReleaseVerifyCircleCI pins what the verify step does with each
// answer of the pipeline list. A project CircleCI does not follow (404 with a
// token) is the first tag of a repository created pull-request-last: the
// repository set-up reconciler follows the project and reports the missed
// build, so the step warns and passes instead of failing every new
// repository's first run. A followed project whose list stays empty is
// triggered by the step itself, and a rejected token still fails the run.
func Test_AutoReleaseVerifyCircleCI(t *testing.T) {
	testCases := []struct {
		name       string
		token      string
		get, post  [2]string
		expectFail bool
		expectOut  []string
		rejectOut  []string
	}{
		{
			name:      "an unfollowed project warns and passes",
			token:     "token",
			get:       [2]string{`{"message":"Project not found"}`, "404"},
			expectOut: []string{"::warning::v0.1.0 is released; CircleCI does not follow this project yet.", "reconciler"},
			rejectOut: []string{"::error::"},
		},
		{
			name:      "no token cannot verify and passes",
			token:     "",
			get:       [2]string{`{"message":"Project not found"}`, "404"},
			expectOut: []string{"::warning::cannot verify that CircleCI built v0.1.0"},
			rejectOut: []string{"::error::"},
		},
		{
			name:      "a followed project with no pipeline is triggered",
			token:     "token",
			get:       [2]string{`{"items":[],"next_page_token":null}`, "200"},
			post:      [2]string{`{"number":7,"state":"pending"}`, "201"},
			expectOut: []string{"triggered CircleCI pipeline #7 for v0.1.0"},
			rejectOut: []string{"::error::", "::warning::"},
		},
		{
			name:       "a rejected token fails",
			token:      "token",
			get:        [2]string{`{"message":"Unauthorized"}`, "401"},
			expectFail: true,
			expectOut:  []string{"::error::CircleCI rejected CIRCLECI_API_TOKEN (HTTP 401)"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runVerify(t, tc.token, tc.get, tc.post)

			if tc.expectFail && err == nil {
				t.Fatalf("verify step passed:\n%s", out)
			}
			if !tc.expectFail && err != nil {
				t.Fatalf("verify step failed: %v\n%s", err, out)
			}
			for _, want := range tc.expectOut {
				if !strings.Contains(string(out), want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, reject := range tc.rejectOut {
				if strings.Contains(string(out), reject) {
					t.Errorf("output carries %q:\n%s", reject, out)
				}
			}
		})
	}
}
