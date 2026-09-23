package prmerge

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// releaseCall records what the merge handed the release wait.
type releaseCall struct {
	owner, repo, sha string
	number           int
}

// fakeRelease is a release wait that fills the result with tag and ends
// with err.
func fakeRelease(calls *[]releaseCall, tag string, err error) ReleaseWait {
	return func(_ context.Context, owner, repo string, number int, sha string, result *releasewait.Result) error {
		*calls = append(*calls, releaseCall{owner: owner, repo: repo, number: number, sha: sha})
		result.Tag, result.SHA = tag, sha
		return err
	}
}

func Test_Merge_release(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		err         error
		wantCode    int
		wantVerdict agentcli.Verdict // the envelope's, for a non-zero code
		wantRelease agentcli.Verdict // the release wait's
		wantReason  string
	}{
		{name: "available", tag: "v1.2.3", wantCode: 0, wantRelease: agentcli.VerdictAvailable},
		{name: "no release follows", err: &releasewait.NoReleaseError{Reason: "the Auto-release run finished without a tag"}, wantCode: 0, wantRelease: agentcli.VerdictNoRelease, wantReason: "finished without a tag"},
		{
			name: "the tag pipeline failed", tag: "v1.2.3",
			err:      agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictCIFailed, "the tag pipeline of v1.2.3 failed: build/push-to-registries-release"),
			wantCode: agentcli.ExitReleaseFailed, wantVerdict: agentcli.VerdictReleaseFailed, wantRelease: agentcli.VerdictCIFailed,
			wantReason: "merged as m1, and the release failed: the tag pipeline of v1.2.3 failed: build/push-to-registries-release",
		},
		{
			name: "timeout", tag: "v1.2.3",
			err:      agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout, "not available within 30m0s: image gsoci.azurecr.io/giantswarm/r:1.2.3"),
			wantCode: agentcli.ExitReleaseUnconfirmed, wantVerdict: agentcli.VerdictReleaseUnconfirmed, wantRelease: agentcli.VerdictTimeout,
			wantReason: "merged as m1, and the release is not confirmed: not available within 30m0s: image gsoci.azurecr.io/giantswarm/r:1.2.3; devctl release wait o/r --pr 42 resumes the wait",
		},
		{
			name:     "the CircleCI token is missing",
			err:      agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired, "CircleCI: no token in the keychain; run devctl auth login --circleci-only"),
			wantCode: agentcli.ExitReleaseUnconfirmed, wantVerdict: agentcli.VerdictReleaseUnconfirmed, wantRelease: agentcli.VerdictAuthRequired,
			wantReason: "run devctl auth login --circleci-only; devctl release wait o/r --pr 42 resumes the wait",
		},
		{
			name: "a tooling failure", err: errors.New("listing tags: connection reset"),
			wantCode: agentcli.ExitReleaseUnconfirmed, wantVerdict: agentcli.VerdictReleaseUnconfirmed, wantRelease: agentcli.VerdictUsage,
			wantReason: "merged as m1, and the release is not confirmed: listing tags: connection reset",
		},
		{
			name:     "superseded by a newer push",
			err:      agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "the Auto-release run was cancelled before it tagged"),
			wantCode: agentcli.ExitReleaseUnconfirmed, wantVerdict: agentcli.VerdictReleaseUnconfirmed, wantRelease: agentcli.VerdictNotApplicable,
			wantReason: "the release is not confirmed: the Auto-release run was cancelled before it tagged",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls []releaseCall
			h := newHarness(t, routes(pull(nil)), func(c *Config) { c.Release = fakeRelease(&calls, tc.tag, tc.err) })
			result, err := h.merge(t)
			if code := exitCode(err); code != tc.wantCode {
				t.Fatalf("want exit %d, got %d: %v", tc.wantCode, code, err)
			}
			if result.MergeCommitSHA != "m1" || !result.BranchDeleted {
				t.Errorf("the merge happened before the release wait: %+v", result)
			}
			if len(calls) != 1 || calls[0] != (releaseCall{owner: "o", repo: "r", number: 42, sha: "m1"}) {
				t.Fatalf("want one release wait for o/r#42 at m1, got %+v", calls)
			}
			if result.Release == nil || result.Release.Verdict != tc.wantRelease || result.Release.Tag != tc.tag || result.Release.SHA != "m1" {
				t.Fatalf("release: %+v", result.Release)
			}
			if tc.wantCode == 0 {
				if !strings.Contains(result.Release.Reason, tc.wantReason) {
					t.Errorf("release reason %q does not contain %q", result.Release.Reason, tc.wantReason)
				}
				return
			}
			if !strings.Contains(exitErrReason(err), result.Release.Reason) {
				t.Errorf("the reason %q carries the release wait's %q", exitErrReason(err), result.Release.Reason)
			}
			var exitErr *agentcli.ExitError
			errors.As(err, &exitErr)
			if exitErr.Verdict != tc.wantVerdict || !strings.Contains(exitErr.Reason, tc.wantReason) {
				t.Errorf("want verdict %s with %q, got %s %q", tc.wantVerdict, tc.wantReason, exitErr.Verdict, exitErr.Reason)
			}
		})
	}
}

// Without a release wait (--no-release-wait) the command ends at the merge
// and the document's release is null.
func Test_Merge_withoutReleaseWait(t *testing.T) {
	h := newHarness(t, routes(pull(nil)), nil)
	result, err := h.merge(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.MergeCommitSHA != "m1" || result.Release != nil {
		t.Errorf("result: %+v", result)
	}
}

// Nothing merged, nothing to release: a red head never reaches the release
// wait.
func Test_Merge_redNeverWaitsForARelease(t *testing.T) {
	red := sequence.Routes{"GET /repos/o/r/commits/abc123/check-runs": {{Body: map[string]any{"total_count": 1, "check_runs": []any{
		map[string]any{"id": 1, "name": "go-build", "status": "completed", "conclusion": "failure", "html_url": "https://github.com/o/r/runs/1"},
	}}}}}
	var calls []releaseCall
	h := newHarness(t, routes(pull(nil), red), func(c *Config) { c.Release = fakeRelease(&calls, "", nil) })
	result, err := h.merge(t)
	if exitCode(err) != agentcli.ExitRed {
		t.Fatalf("want exit 1, got %v", err)
	}
	if len(calls) != 0 || result.Release != nil {
		t.Errorf("want no release wait, got %+v and %+v", calls, result.Release)
	}
}

// A head in a fork keeps its branch, and the release still follows.
func Test_Merge_forkHeadWaitsForTheRelease(t *testing.T) {
	pr := pull(map[string]any{"head": map[string]any{"sha": "abc123", "ref": "feature", "repo": map[string]any{"full_name": "alice/r"}}})
	r := routes(pr)
	maps.DeleteFunc(r, func(key string, _ []sequence.Response) bool { return strings.HasPrefix(key, "DELETE ") })
	var calls []releaseCall
	h := newHarness(t, r, func(c *Config) { c.Release = fakeRelease(&calls, "v1.2.3", nil) })
	result, err := h.merge(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.BranchDeleted || len(calls) != 1 || result.Release == nil || result.Release.Verdict != agentcli.VerdictAvailable {
		t.Errorf("result: %+v, calls %+v", result, calls)
	}
}

func exitErrReason(err error) string {
	var exitErr *agentcli.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Reason
	}
	return ""
}
