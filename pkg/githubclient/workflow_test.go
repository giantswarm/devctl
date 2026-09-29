package githubclient

import (
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

func TestDispatchWorkflowForbidden(t *testing.T) {
	c, _ := newWireClient(t, http.StatusForbidden, `{"message":"Resource not accessible by integration"}`)
	err := c.DispatchWorkflow(t.Context(), "o", "r", "zz_generated.auto_release.yaml", "main", nil)
	require.Error(t, err)
	require.True(t, IsForbidden(err), "a masked 403 is forbidden: %v", err)
}

func TestHasWorkflow(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `{"id":1,"path":".github/workflows/zz_generated.auto_release.yaml","state":"active"}`)
	found, err := c.HasWorkflow(t.Context(), "o", "r", "zz_generated.auto_release.yaml")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "/repos/o/r/actions/workflows/zz_generated.auto_release.yaml", got.path)

	c, _ = newWireClient(t, http.StatusNotFound, `{"message":"Not Found"}`)
	found, err = c.HasWorkflow(t.Context(), "o", "r", "zz_generated.auto_release.yaml")
	require.NoError(t, err)
	require.False(t, found)
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
