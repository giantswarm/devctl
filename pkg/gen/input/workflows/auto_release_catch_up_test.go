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
	_, log, err = runStep(t, tagJobStep(t, "catch-up").Run, dir, path, "DISPATCH_LOG="+calls, "GH_REPO=giantswarm/example", "TARGET="+stable["target"])
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
			env := []string{path, "DISPATCH_LOG=" + calls, "GH_REPO=giantswarm/example", "GITHUB_REF_NAME=" + tc.branch, "TARGET=" + target}
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
