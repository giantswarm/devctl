package githubclient

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDispatchWorkflowWire(t *testing.T) {
	c, got := newWireClient(t, http.StatusNoContent, "")
	err := c.DispatchWorkflow(t.Context(), "o", "r", "zz_generated.auto_release.yaml", "main", map[string]any{"release-type": "stable"})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/repos/o/r/actions/workflows/zz_generated.auto_release.yaml/dispatches", got.path)
	require.Equal(t, map[string]any{"ref": "main", "inputs": map[string]any{"release-type": "stable"}}, got.body)
}

func TestReadFile(t *testing.T) {
	content := base64.StdEncoding.EncodeToString([]byte("name: Auto release\n"))
	c, got := newWireClient(t, http.StatusOK, `{"type":"file","encoding":"base64","path":".github/workflows/zz_generated.auto_release.yaml","content":"`+content+`"}`)
	data, found, err := c.ReadFile(t.Context(), "o", "r", ".github/workflows/zz_generated.auto_release.yaml", "main")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "name: Auto release\n", string(data))
	require.Equal(t, "/repos/o/r/contents/.github/workflows/zz_generated.auto_release.yaml", got.path)
	require.Equal(t, "ref=main", got.query)

	c, _ = newWireClient(t, http.StatusNotFound, `{"message":"Not Found"}`)
	_, found, err = c.ReadFile(t.Context(), "o", "r", ".github/workflows/zz_generated.auto_release.yaml", "main")
	require.NoError(t, err)
	require.False(t, found)
}

// TestReachable pins the comparison: the tag is the base and the branch the
// head, so a tag in the branch's history leaves the branch ahead of it or
// identical to it.
func TestReachable(t *testing.T) {
	for status, want := range map[string]bool{"ahead": true, "identical": true, "behind": false, "diverged": false} {
		t.Run(status, func(t *testing.T) {
			c, got := newWireClient(t, http.StatusOK, `{"status":"`+status+`","ahead_by":0,"behind_by":0}`)
			reachable, err := c.Reachable(t.Context(), "o", "r", "v1.3.0-rc.1", "main")
			require.NoError(t, err)
			require.Equal(t, want, reachable)
			require.Equal(t, http.MethodGet, got.method)
			require.Equal(t, "/repos/o/r/compare/v1.3.0-rc.1...main", got.path)
		})
	}
}

func TestGetCombinedStatus(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `{"state":"failure","total_count":3,"statuses":[]}`)
	status, err := c.GetCombinedStatus(t.Context(), "o", "r", "v1.2.0-rc.1")
	require.NoError(t, err)
	require.Equal(t, CombinedStatus{State: "failure", TotalCount: 3}, status)
	require.Equal(t, "/repos/o/r/commits/v1.2.0-rc.1/status", got.path)
}

func TestListReleases(t *testing.T) {
	c, _ := newWireClient(t, http.StatusOK, `[{"tag_name":"v1.2.0-rc.1","prerelease":true},{"tag_name":"v1.1.0","draft":true}]`)
	releases, err := c.ListReleases(t.Context(), "o", "r")
	require.NoError(t, err)
	require.Equal(t, []Release{{Tag: "v1.2.0-rc.1", Published: true, Prerelease: true}, {Tag: "v1.1.0"}}, releases)
}

// TestCommitsAfter pins the comparison: the merge commit is the base and the
// branch the head, and only a branch ahead of it has commits after it.
func TestCommitsAfter(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `{"status":"ahead","commits":[{"sha":"c1"},{"sha":"c2"}]}`)
	commits, err := c.CommitsAfter(t.Context(), "o", "r", "m1", "release-v1.1.x")
	require.NoError(t, err)
	require.Equal(t, []string{"c1", "c2"}, commits)
	require.Equal(t, "/repos/o/r/compare/m1...release-v1.1.x", got.path)

	for _, status := range []string{"identical", "behind", "diverged"} {
		c, _ := newWireClient(t, http.StatusOK, `{"status":"`+status+`","commits":[{"sha":"c1"}]}`)
		commits, err := c.CommitsAfter(t.Context(), "o", "r", "m1", "release-v1.1.x")
		require.NoError(t, err)
		require.Empty(t, commits, status)
	}

	c, _ = newWireClient(t, http.StatusNotFound, `{"message":"Not Found"}`)
	commits, err = c.CommitsAfter(t.Context(), "o", "r", "m1", "gone")
	require.NoError(t, err)
	require.Empty(t, commits)
}

func TestCommitTags(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `[
		{"ref":"refs/tags/v1.1.4","object":{"type":"commit","sha":"c1"}},
		{"ref":"refs/tags/v1.1.4-rc.1","object":{"type":"commit","sha":"c1"}},
		{"ref":"refs/tags/v2.0.0","object":{"type":"tag","sha":"t1"}}
	]`)
	tags, err := c.CommitTags(t.Context(), "o", "r")
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"c1": {"v1.1.4", "v1.1.4-rc.1"}}, tags)
	require.Equal(t, "/repos/o/r/git/matching-refs/tags/", got.path)
}

func TestNewestTag(t *testing.T) {
	require.Equal(t, "", NewestTag(nil))
	require.Equal(t, "v1.1.4", NewestTag([]string{"v1.1.4-rc.1", "v1.1.4", "v1.1.3"}))
	require.Equal(t, "v1.0.0", NewestTag([]string{"latest", "v1.0.0"}))
	require.Equal(t, "latest", NewestTag([]string{"latest", "nightly"}))
}
