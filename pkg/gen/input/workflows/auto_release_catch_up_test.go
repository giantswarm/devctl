package workflows

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

func catchUpDispatchStub(t *testing.T) (path, calls string) {
	t.Helper()
	bin := t.TempDir()
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$GH_REPO" "$@" > "$DISPATCH_LOG"
exit "${DISPATCH_EXIT:-0}"
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o700)) // #nosec G306 -- test-only executable stub
	return "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), filepath.Join(t.TempDir(), "dispatch")
}

func Test_AutoReleaseCatchUpOrder(t *testing.T) {
	for _, flavour := range []gen.Flavour{gen.FlavourApp, gen.FlavourFork} {
		t.Run(string(flavour), func(t *testing.T) {
			steps, rendered := tagJobStepsFor(t, flavour)
			var ids []string
			for _, step := range steps {
				ids = append(ids, step.ID)
			}
			at := slices.Index(ids, "catch-up")
			require.Greater(t, at, slices.Index(ids, "release"))
			require.Less(t, at, len(steps)-1, "catch-up must precede CircleCI verification")
			require.Equal(t, "inputs.release-type == 'stable' && steps.release.outcome == 'success'", steps[at].If)
			require.Equal(t, "${{ steps.decide.outputs.target }}", steps[at].Env["TARGET"])
			require.Equal(t, "${{ secrets.GITHUB_TOKEN }}", steps[at].Env["GH_TOKEN"])
			require.Equal(t, "${{ github.repository }}", steps[at].Env["GH_REPO"])
			require.Equal(t, "${{ steps.decide.outputs.tag }}", steps[at].Env["STABLE"])
			for _, id := range []string{"catch-up-token", "catch-up-pr"} {
				index := slices.Index(ids, id)
				require.Greater(t, index, at)
				require.Equal(t, "steps.catch-up.outputs.prepare == 'true'", steps[index].If)
			}
			token := steps[slices.Index(ids, "catch-up-token")]
			require.Contains(t, token.Uses, "actions/create-github-app-token@")
			require.Equal(t, "${{ secrets.RELEASE_BOT_CLIENT_ID }}", token.With["client-id"])
			require.Equal(t, "${{ secrets.RELEASE_BOT_PRIVATE_KEY }}", token.With["private-key"])
			require.Equal(t, "write", token.With["permission-contents"])
			require.Equal(t, "write", token.With["permission-pull-requests"])
			prepare := steps[slices.Index(ids, "catch-up-pr")]
			require.Equal(t, "${{ steps.catch-up-token.outputs.token }}", prepare.Env["GH_TOKEN"])
			var checkedOut bool
			for _, step := range steps {
				if step.Name == "Check out the catch-up head" {
					checkedOut = true
					require.Equal(t, "steps.catch-up.outputs.prepare == 'true'", step.If)
					require.Equal(t, "${{ steps.catch-up.outputs.head }}", step.With["ref"])
					require.Equal(t, "${{ steps.catch-up-token.outputs.token }}", step.With["token"])
					require.Equal(t, step.With["path"], prepare.WorkingDirectory)
					require.NotEmpty(t, prepare.WorkingDirectory, "preserve the promoted checkout for CircleCI verification")
				}
			}
			require.True(t, checkedOut, "the preparation branch needs the App token to trigger CI")

			var workflow struct {
				Jobs map[string]struct {
					Permissions map[string]string `yaml:"permissions"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(rendered), &workflow))
			require.Equal(t, "write", workflow.Jobs["tag"].Permissions["actions"])
		})
	}
}

// Reproduce the reported ordering with real Git and git-cliff: rc.3, rc.4,
// another feature merged after the promotion's checkout, then rc.3 promoted.
// Only gh is stubbed. The queued run reads the new stable as its baseline
// and cuts the next minor candidate containing both excluded features.
func Test_AutoReleaseCatchUpAfterPromotingEarlierRC(t *testing.T) {
	requireGitCliff(t)
	origin := repo(t, "v0.121.0", "feat: proven", "v0.122.0-rc.3", "feat: later candidate", "v0.122.0-rc.4")
	dir := filepath.Join(t.TempDir(), "checkout")
	mustGit(t, origin, "clone", "--no-local", origin, dir)
	promoted, log, err := runStep(t, promoteScript(t), dir, builtCandidate(t), "GH_REPO=giantswarm/example", "PROMOTE_VERSION=v0.122.0-rc.3")
	require.NoError(t, err, log)
	cliffContext(t, dir)
	stable, log, err := runStep(t, decideScript(t), dir, "NEXT=v0.122.0", "WANT=stable", "PROMOTE="+promoted["rc"], "CLIFF_CONTEXT=cliff-context.json")
	require.NoError(t, err, log)
	require.Equal(t, "v0.122.0", stable["tag"])
	require.Equal(t, strings.TrimSpace(mustGit(t, origin, "rev-parse", "v0.122.0-rc.3")), stable["target"])

	// The remote advances after checkout and the stable is published there.
	mustGit(t, origin, "commit", "--allow-empty", "-m", "feat: merged after rc.4")
	head := strings.TrimSpace(mustGit(t, origin, "rev-parse", "HEAD"))
	mustGit(t, origin, "tag", stable["tag"], stable["target"])
	path, calls := catchUpDispatchStub(t)
	_, log, err = runStep(t, tagJobStep(t, "catch-up").Run, dir, path, "DISPATCH_LOG="+calls, "GH_REPO=giantswarm/example", "TARGET="+stable["target"], "STABLE="+stable["tag"])
	require.NoError(t, err, log)
	require.Contains(t, log, "feat: merged after rc.4")
	raw, err := os.ReadFile(calls) // #nosec G304 -- dispatch log path built from t.TempDir
	require.NoError(t, err)
	require.Equal(t, "giantswarm/example\nworkflow\nrun\nzz_generated.auto_release.yaml\n--ref\nmain\n-f\nrelease-type=auto\n", string(raw))

	// A new run checks out main, not the detached promoted candidate.
	nextDir := filepath.Join(t.TempDir(), "next-run")
	mustGit(t, origin, "clone", "--no-local", origin, nextDir)
	cliffTomlIn(t, nextDir)
	computed, log, err := runStep(t, tagJobStepNamed(t, "Compute next version").Run, nextDir)
	require.NoError(t, err, log)
	require.Equal(t, "v0.123.0", computed["version"])
	context, err := os.ReadFile(filepath.Join(nextDir, "cliff-context.json")) // #nosec G304 -- context path built from t.TempDir
	require.NoError(t, err)
	require.Contains(t, string(context), "feat: later candidate")
	require.Contains(t, string(context), "feat: merged after rc.4")
	require.NotContains(t, string(context), "feat: proven")
	next, log, err := runStep(t, decideScript(t), nextDir, "NEXT="+computed["version"], "WANT=auto", "CLIFF_CONTEXT=cliff-context.json")
	require.NoError(t, err, log)
	require.Equal(t, "v0.123.0-rc.1", next["tag"])
	require.Equal(t, "true", next["prerelease"])
	require.Equal(t, head, next["target"])
	_, log, err = runStep(t, tagJobStep(t, "one-version").Run, nextDir, "TAG="+next["tag"], "TARGET="+head)
	require.NoError(t, err, log)
}

// No user commit follows rc.4. Only GitHub's PR and release endpoints are
// stubbed; the workflow scripts run against real Git and pinned git-cliff.
func Test_AutoReleaseCatchUpFromTaggedHead(t *testing.T) {
	requireGitCliff(t)
	origin := repo(t, "v0.121.0", "feat: proven", "v0.122.0-rc.3", "feat: later candidate", "v0.122.0-rc.4")
	head := strings.TrimSpace(mustGit(t, origin, "rev-parse", "HEAD"))
	target := strings.TrimSpace(mustGit(t, origin, "rev-parse", "v0.122.0-rc.3"))
	mustGit(t, origin, "tag", "v0.122.0", target)
	dir := filepath.Join(t.TempDir(), "promotion")
	mustGit(t, origin, "clone", "--no-local", origin, dir)
	mustGit(t, dir, "checkout", "--detach", target)
	path, calls := catchUpDispatchStub(t)
	caught, log, err := runStep(t, tagJobStep(t, "catch-up").Run, dir, path, "DISPATCH_LOG="+calls,
		"TARGET="+target, "STABLE=v0.122.0")
	require.NoError(t, err, log)
	require.Equal(t, "true", caught["prepare"])
	require.Equal(t, head, caught["head"])
	_, err = os.Stat(calls)
	require.ErrorIs(t, err, os.ErrNotExist, "do not dispatch onto the conflicting head")

	bin := t.TempDir()
	stub := `#!/usr/bin/env bash
set -euo pipefail
case "$1 $2" in
  "pr list") [ ! -f "$PR_LOG" ] || printf '%s\n' 'https://github.com/giantswarm/example/pull/1' ;;
  "pr create") printf '%s\n' "$@" > "$PR_LOG"; echo 'https://github.com/giantswarm/example/pull/1' ;;
  "pr view") echo "${PR_STATE:-OPEN}" ;;
  "pr merge") printf '%s\n' "$@" > "$MERGE_LOG"; exit "${MERGE_EXIT:-0}" ;;
  "release create")
    printf '%s\n' "$@" > "$RELEASE_LOG"
    git -C "$ORIGIN" tag "$3" "$TARGET"
    ;;
  *) echo "unexpected gh $*" >&2; exit 2 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o700)) // #nosec G306 -- test-only executable
	prLog, mergeLog := filepath.Join(t.TempDir(), "pr"), filepath.Join(t.TempDir(), "merge")
	env := []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"STABLE=v0.122.0", "CATCH_UP_HEAD=" + head, "PR_LOG=" + prLog, "MERGE_LOG=" + mergeLog,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}
	_, log, err = runStep(t, tagJobStep(t, "catch-up-pr").Run, dir, env...)
	require.NoError(t, err, log)
	branch := "release/catch-up-v0.122.0-" + head[:12]
	prepared := strings.TrimSpace(mustGit(t, origin, "rev-parse", branch))
	require.NotEqual(t, head, prepared)
	require.Equal(t, head, strings.TrimSpace(mustGit(t, origin, "rev-parse", prepared+"^")))
	require.Equal(t, head, strings.TrimSpace(mustGit(t, origin, "rev-parse", "main")), "the workflow never pushes to main")
	require.Equal(t, ".github/release-catch-up", strings.TrimSpace(mustGit(t, origin, "diff", "--name-only", head, prepared)))
	raw, err := os.ReadFile(mergeLog) // #nosec G304 -- temporary fixture log
	require.NoError(t, err)
	require.Contains(t, string(raw), "--auto\n--squash\n", "merge must wait for required checks")
	// A push succeeded but PR creation failed: the next run reuses the
	// remote branch even though its PR lookup still returns nothing.
	require.NoError(t, os.Remove(prLog))
	_, log, err = runStep(t, tagJobStep(t, "catch-up-pr").Run, dir, env...)
	require.NoError(t, err, log)
	require.Equal(t, prepared, strings.TrimSpace(mustGit(t, origin, "rev-parse", branch)))

	// Reruns reuse the published PR instead of rewriting its branch.
	_, log, err = runStep(t, tagJobStep(t, "catch-up-pr").Run, dir, env...)
	require.NoError(t, err, log)
	require.Equal(t, prepared, strings.TrimSpace(mustGit(t, origin, "rev-parse", branch)))
	_, log, err = runStep(t, tagJobStep(t, "catch-up-pr").Run, dir, append(env, "PR_STATE=MERGED")...)
	require.NoError(t, err, log)
	require.Contains(t, log, "already merged")
	_, log, err = runStep(t, tagJobStep(t, "catch-up-pr").Run, dir, append(env, "PR_STATE=CLOSED")...)
	require.Error(t, err, log)
	require.Contains(t, log, "closed without merging")
	_, log, err = runStep(t, tagJobStep(t, "catch-up-pr").Run, dir, append(env, "MERGE_EXIT=1")...)
	require.Error(t, err, "a refused auto-merge must fail catch-up: %s", log)

	// Simulate GitHub merging after required checks. That push triggers the
	// next run; no user push and no tag-guard exception are needed.
	mustGit(t, origin, "merge", "--ff-only", branch)
	nextDir := filepath.Join(t.TempDir(), "next-run")
	mustGit(t, origin, "clone", "--no-local", origin, nextDir)
	cliffTomlIn(t, nextDir)
	computed, log, err := runStep(t, tagJobStepNamed(t, "Compute next version").Run, nextDir)
	require.NoError(t, err, log)
	require.Equal(t, "v0.123.0", computed["version"])
	context, err := os.ReadFile(filepath.Join(nextDir, "cliff-context.json")) // #nosec G304 -- temporary fixture context
	require.NoError(t, err)
	require.Contains(t, string(context), "feat: later candidate")
	require.NotContains(t, string(context), "feat: proven")
	next, log, err := runStep(t, decideScript(t), nextDir, "NEXT="+computed["version"], "WANT=auto", "CLIFF_CONTEXT=cliff-context.json")
	require.NoError(t, err, log)
	require.Equal(t, "v0.123.0-rc.1", next["tag"])
	require.Equal(t, prepared, next["target"])
	_, log, err = runStep(t, tagJobStep(t, "one-version").Run, nextDir, "TAG="+next["tag"], "TARGET="+next["target"])
	require.NoError(t, err, log)
	_, log, err = runStep(t, tagJobStep(t, "notes").Run, nextDir, "TAG="+next["tag"])
	require.NoError(t, err, log)
	releaseLog := filepath.Join(t.TempDir(), "release")
	_, log, err = runStep(t, tagJobStep(t, "release").Run, nextDir, env[0], "ORIGIN="+origin, "RELEASE_LOG="+releaseLog,
		"TAG="+next["tag"], "TARGET="+next["target"], "PRERELEASE=true")
	require.NoError(t, err, log)
	require.Equal(t, prepared, strings.TrimSpace(mustGit(t, origin, "rev-parse", next["tag"])))
}

func Test_AutoReleaseCatchUpDispatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		branch   string
		advance  bool
		diverge  bool
		fail     bool
		wantCall bool
		wantErr  bool
	}{
		{name: "nothing after the promoted commit", branch: "main"},
		{name: "a later push is fetched", branch: "main", advance: true, wantCall: true},
		{name: "a maintenance branch uses its own ref", branch: "release-1.2", advance: true, wantCall: true},
		{name: "a dispatch failure fails the step", branch: "main", advance: true, fail: true, wantCall: true, wantErr: true},
		{name: "a rewritten branch is refused", branch: "main", diverge: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origin := repo(t, "v1.1.0", "feat: proven", "v1.2.0-rc.1")
			if tc.branch != "main" {
				mustGit(t, origin, "branch", "-m", tc.branch)
			}
			target := strings.TrimSpace(mustGit(t, origin, "rev-parse", "HEAD"))
			dir := filepath.Join(t.TempDir(), "checkout")
			mustGit(t, origin, "clone", "--no-local", origin, dir)
			mustGit(t, dir, "checkout", "--detach", target)
			if tc.advance {
				mustGit(t, origin, "commit", "--allow-empty", "-m", "feat: late")
			}
			if tc.diverge {
				mustGit(t, origin, "checkout", "--orphan", "rewritten")
				mustGit(t, origin, "commit", "--allow-empty", "-m", "feat: unrelated")
				mustGit(t, origin, "branch", "-f", tc.branch, "HEAD")
			}
			path, calls := catchUpDispatchStub(t)
			env := []string{path, "DISPATCH_LOG=" + calls, "GH_REPO=giantswarm/example", "GITHUB_REF_NAME=" + tc.branch, "TARGET=" + target, "STABLE=v1.2.0"}
			if tc.fail {
				env = append(env, "DISPATCH_EXIT=1")
			}
			_, log, err := runStep(t, tagJobStep(t, "catch-up").Run, dir, env...)
			require.Equal(t, tc.wantErr, err != nil, log)
			raw, readErr := os.ReadFile(calls) // #nosec G304 -- dispatch log path built from t.TempDir
			if tc.wantCall {
				require.NoError(t, readErr)
				require.Contains(t, string(raw), "--ref\n"+tc.branch+"\n")
			} else {
				require.ErrorIs(t, readErr, os.ErrNotExist)
			}
		})
	}
}
