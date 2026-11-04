package githubclient

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetPullRequestMergeBase(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `{"number":7,"state":"closed","merged":true,"merge_commit_sha":"merge-sha","base":{"ref":"release-1.3"}}`)
	merge, err := c.GetPullRequestMerge(t.Context(), "o", "r", 7)
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/repos/o/r/pulls/7", got.path)
	require.Equal(t, PullRequestMerge{Number: 7, State: "closed", Merged: true, MergeCommitSHA: "merge-sha", BaseRef: "release-1.3"}, merge)
}
