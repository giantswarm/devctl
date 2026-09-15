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

	rendered := renderInput(t, newWorkflows(t, gen.FlavourApp).AutoRelease())

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
func gitIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
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
	if last, err := gitIn(t, dir, "describe", "--tags", "--abbrev=0", "--match=v*.*.*", "--exclude=*-*"); err == nil {
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

	outPath := filepath.Join(t.TempDir(), "github-output")
	if err := os.WriteFile(outPath, nil, 0o600); err != nil {
		t.Fatalf("seed GITHUB_OUTPUT: %v", err)
	}

	cliffContext(t, dir)

	cmd := exec.CommandContext(t.Context(), "bash", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"NEXT="+next,
		"WANT="+want,
		"CLIFF_CONTEXT=cliff-context.json",
		"GITHUB_REF_NAME=main",
		"GITHUB_OUTPUT="+outPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("decide step failed: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(outPath) // #nosec G304 -- path built from t.TempDir
	if err != nil {
		t.Fatalf("read GITHUB_OUTPUT: %v", err)
	}

	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			got[key] = value
		}
	}

	return got
}

// Test_AutoReleaseDecide pins the release-candidate rule: tag an RC when at
// least one unreleased commit is feat-rc/fix-rc and no unreleased feat, fix or
// breaking commit lacks the -rc. `next` is what git-cliff computes, which is
// always the stable target because cliff.toml normalises the -rc away.
func Test_AutoReleaseDecide(t *testing.T) {
	script := decideScript(t)

	testCases := []struct {
		name           string
		history        []string
		next           string
		want           string
		expectTag      string
		expectPrerelse string
	}{
		{
			name:           "case 0: no marker anywhere is a stable release",
			history:        []string{"v1.2.9", "feat: add x"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0",
			expectPrerelse: "false",
		},
		{
			name:           "case 1: a single feat-rc opens the cycle",
			history:        []string{"v1.2.9", "feat-rc: add x"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0-rc.1",
			expectPrerelse: "true",
		},
		{
			name:           "case 2: chore(deps) does not decide, so the cycle holds",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "chore(deps): bump y"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0-rc.2",
			expectPrerelse: "true",
		},
		{
			// `docs` neither decides nor releases: cliff.toml skips it, so
			// there is nothing new to put in a candidate and the cycle waits.
			name:      "case 3: a docs-only push during a cycle releases nothing",
			history:   []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "docs: fix typo"},
			next:      "v1.3.0",
			expectTag: "",
		},
		{
			name:      "a non-conventional push during a cycle releases nothing",
			history:   []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "Merge pull request #12 from foo/bar"},
			next:      "v1.3.0",
			expectTag: "",
		},
		{
			name:           "case 4: an unmarked fix closes the cycle at the stable target",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "fix: last thing"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0",
			expectPrerelse: "false",
		},
		{
			name:           "case 5: an unmarked breaking change of any type closes the cycle",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "refactor!: drop y"},
			next:           "v2.0.0",
			expectTag:      "v2.0.0",
			expectPrerelse: "false",
		},
		{
			// git-cliff bumps the major on a `BREAKING CHANGE:` footer as
			// readily as on `!`, so the footer has to close the cycle too.
			// Otherwise the cycle ships rc.N of a major nobody marked.
			name:           "case 5b: an unmarked breaking footer closes the cycle",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "refactor: drop the v1 API\n\nBREAKING CHANGE: v1 is gone"},
			next:           "v2.0.0",
			expectTag:      "v2.0.0",
			expectPrerelse: "false",
		},
		{
			name:           "the BREAKING-CHANGE spelling closes it as well",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "chore: tidy\n\nBREAKING-CHANGE: gone"},
			next:           "v2.0.0",
			expectTag:      "v2.0.0",
			expectPrerelse: "false",
		},
		{
			// The marked commit stays marked: it carries the -rc, so its own
			// footer must not count against it.
			name:           "a marked breaking footer keeps the cycle open",
			history:        []string{"v1.2.9", "feat-rc: rework\n\nBREAKING CHANGE: v1 is gone"},
			next:           "v2.0.0",
			expectTag:      "v2.0.0-rc.1",
			expectPrerelse: "true",
		},
		{
			name:           "a marked breaking change gives a major RC",
			history:        []string{"v1.2.9", "feat-rc!: drop v1"},
			next:           "v2.0.0",
			expectTag:      "v2.0.0-rc.1",
			expectPrerelse: "true",
		},
		{
			name:           "scopes are accepted on both sides of the rule",
			history:        []string{"v1.2.9", "feat-rc(auth): add x", "v1.3.0-rc.1", "chore(deps): bump y"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0-rc.2",
			expectPrerelse: "true",
		},
		{
			name:           "rc.9 is followed by rc.10, not rc.2",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.9", "fix-rc: tweak"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0-rc.10",
			expectPrerelse: "true",
		},
		{
			name:           "a moved target restarts the RC series at rc.1",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "feat-rc!: drop v1"},
			next:           "v2.0.0",
			expectTag:      "v2.0.0-rc.1",
			expectPrerelse: "true",
		},
		{
			name:           "inception, with no tag at all",
			history:        []string{"feat-rc: add x"},
			next:           "v0.1.0",
			expectTag:      "v0.1.0-rc.1",
			expectPrerelse: "true",
		},
		{
			name:      "no releasable commits, so no tag",
			history:   []string{"v1.3.0"},
			next:      "v1.3.0",
			expectTag: "",
		},
		{
			// The stable target equals the last stable tag, but the RC series
			// is not finished, so an explicit `rc` must still produce one.
			name:           "workflow_dispatch rc forces one more candidate",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1"},
			next:           "v1.3.0",
			want:           "rc",
			expectTag:      "v1.3.0-rc.2",
			expectPrerelse: "true",
		},
		{
			name:           "workflow_dispatch stable closes a cycle with nothing left to merge",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1"},
			next:           "v1.3.0",
			want:           "stable",
			expectTag:      "v1.3.0",
			expectPrerelse: "false",
		},
		{
			// On release-2.x the reachable baseline is v2.3.5, not the higher
			// v3.x tags on main, which git-cliff has already accounted for in
			// `next`. The step must agree, or it reports "nothing to release".
			name:           "a backport branch bumps from its own reachable baseline",
			history:        []string{"v2.3.5", "fix: backport"},
			next:           "v2.3.6",
			expectTag:      "v2.3.6",
			expectPrerelse: "false",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			if want == "" {
				want = "auto"
			}

			got := decide(t, script, repo(t, tc.history...), tc.next, want)

			if got["tag"] != tc.expectTag {
				t.Errorf("tag = %q, want %q", got["tag"], tc.expectTag)
			}
			if got["prerelease"] != tc.expectPrerelse {
				t.Errorf("prerelease = %q, want %q", got["prerelease"], tc.expectPrerelse)
			}
		})
	}
}

// Test_AutoReleaseDecideIgnoresUnmergedCandidates pins that the rc counter is
// scoped to reachable tags. The describe baseline is scoped for the backport
// case; a candidate for the same target cut on another branch must not shift
// the numbering on this one either.
func Test_AutoReleaseDecideIgnoresUnmergedCandidates(t *testing.T) {
	script := decideScript(t)

	dir := repo(t, "v1.2.9", "feat-rc: add x")

	// A candidate cut on a side branch that never merged. cliff.toml keeps it
	// out of git-cliff's baseline through use_branch_tags, so `NEXT` below is
	// still v1.3.0; the step has to agree.
	mustGit(t, dir, "checkout", "-q", "-b", "side", "v1.2.9")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "feat-rc: something else")
	mustGit(t, dir, "tag", "v1.3.0-rc.7")
	mustGit(t, dir, "checkout", "-q", "main")

	got := decide(t, script, dir, "v1.3.0", "auto")

	if got["tag"] != "v1.3.0-rc.1" {
		t.Errorf("tag = %q, want v1.3.0-rc.1: the unmerged v1.3.0-rc.7 must not count", got["tag"])
	}
}

// Test_AutoReleaseDescribeExcludesPreReleases pins the --exclude flag. Without
// it `git describe --match='v*.*.*'` returns a reachable v1.3.0-rc.N as the
// baseline, the "nothing to release" comparison can never be true again, and
// the job tags on every push.
func Test_AutoReleaseDescribeExcludesPreReleases(t *testing.T) {
	script := decideScript(t)

	if !strings.Contains(script, "--exclude='*-*'") {
		t.Errorf("decide step must exclude pre-release tags from the describe baseline:\n%s", script)
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

// Test_SemanticPullRequestAcceptsRcTypes pins the other half of that contract.
// The action's stock parser reads the type with `\w*`, so the widened
// header_pattern is what lets a hyphenated type through at all; `types` alone
// would not.
func Test_SemanticPullRequestAcceptsRcTypes(t *testing.T) {
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

	if with["header_pattern"] != `^(\w*(?:-rc)?)(?:\((.*)\))?!?: (.*)$` {
		t.Errorf("header_pattern = %q, want the type group widened by exactly the -rc suffix", with["header_pattern"])
	}

	// `types` replaces the action's default list rather than extending it, so
	// dropping one of these silently blocks that type on every repository.
	// `security` is not one of the action's defaults, so it needs the explicit
	// list to be accepted at all; cliff.toml maps it to the Security group.
	for _, want := range []string{
		"feat(-rc)?", "fix(-rc)?", "docs", "style", "refactor",
		"perf", "test", "build", "ci", "chore", "revert", "security",
	} {
		if !strings.Contains(with["types"], want+"\n") {
			t.Errorf("types is missing %q:\n%s", want, with["types"])
		}
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

	cmd := exec.CommandContext(t.Context(), "bash", "-c", script)
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
