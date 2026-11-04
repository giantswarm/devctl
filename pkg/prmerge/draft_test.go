package prmerge

import (
	"net/http"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func Test_Merge_draftAfterWait(t *testing.T) {
	// The PR becomes a draft after the wait read a green, ready head. The
	// final gate must still refuse it before any merge or branch deletion.
	r := routes(pull(nil), sequence.Routes{
		"GET /repos/o/r/pulls/42": {
			{Body: pull(nil)},
			{Body: pull(nil)},
			{Body: pull(map[string]any{"draft": true})},
		},
	})
	h := newHarness(t, r, nil)
	result, err := h.merge(t)
	if code := exitCode(err); code != agentcli.ExitNotApplicable || !strings.Contains(err.Error(), "a draft") {
		t.Fatalf("want exit 3 naming the draft, got %d: %v", code, err)
	}
	if !result.Draft || result.MergeCommitSHA != "" || result.Enqueued || result.BranchDeleted {
		t.Errorf("want an unmerged draft, got %+v", result)
	}
	if h.requested("GET /repos/o/r/commits/abc123/check-runs") == 0 {
		t.Error("the fixture must reach the wait before the PR becomes a draft")
	}
	for _, req := range h.server.Requests() {
		if req.Method != http.MethodGet {
			t.Errorf("draft refusal must not write: %s", req.String())
		}
	}
}
