package releasewait

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

func TestRecordPublishStatusOnlyVerifiedTagOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tag      string
		pipeline *Pipeline
		outcome  error
		wantRead bool
		wantMark bool
	}{
		{name: "failed tag", tag: testTag, pipeline: &Pipeline{ID: "p1", URL: "https://example.com/p1"}, outcome: ciFailedErr("go-build failed"), wantRead: true, wantMark: true},
		{name: "recovered tag", tag: testTag, pipeline: &Pipeline{ID: "p1"}, wantRead: true},
		{name: "branch status failed after tag became available", tag: testTag, pipeline: &Pipeline{ID: "p1"}, outcome: statusFailedErr("branch status failed"), wantRead: true},
		{name: "pipeline unverified on shared commit", tag: testTag, pipeline: &Pipeline{}, outcome: ciFailedErr("commit status failed")},
		{name: "status-only success on shared commit", tag: testTag, pipeline: &Pipeline{}},
		{name: "Actions-only", tag: testTag, outcome: ciFailedErr("Actions failed")},
		{name: "before tag created", pipeline: &Pipeline{ID: "p1"}, outcome: ciFailedErr("auto-release failed")},
		{name: "timeout", tag: testTag, pipeline: &Pipeline{ID: "p1"}, outcome: timeoutErr("still running")},
		{name: "authentication", tag: testTag, pipeline: &Pipeline{ID: "p1"}, outcome: agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired, "no CircleCI login")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, patches := 0, 0
			body := "<!-- devctl:publish-failed -->\nPrevious failure\n<!-- /devctl:publish-failed -->\n\nOriginal notes"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					reads++
					require.Equal(t, "/repos/giantswarm/kserve/releases/tags/"+testTag, r.URL.Path)
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "body": body})
				} else {
					patches++
					var patch map[string]string
					require.NoError(t, json.NewDecoder(r.Body).Decode(&patch))
					body = patch["body"]
					_, _ = io.WriteString(w, `{}`)
				}
			}))
			t.Cleanup(server.Close)
			c, _, err := githubclient.NewConditional(githubclient.Config{Logger: discardLogger(), AccessToken: "ghu_test", BaseURL: server.URL})
			require.NoError(t, err)
			result := Result{Repository: testOwner + "/" + testRepo, Tag: tc.tag, Pipeline: tc.pipeline}
			require.NoError(t, RecordPublishStatus(t.Context(), c, result, tc.outcome))
			if tc.wantRead {
				require.Equal(t, 1, reads)
				require.Equal(t, 1, patches)
				if tc.wantMark {
					require.Contains(t, body, "**Not published:**")
				} else {
					require.Equal(t, "Original notes", body)
				}
			} else {
				require.Zero(t, reads)
				require.Zero(t, patches)
			}
		})
	}
}

func TestRecordPublishStatusHonorsCancellation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	c, _, err := githubclient.NewConditional(githubclient.Config{Logger: discardLogger(), AccessToken: "ghu_test", BaseURL: server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := Result{Repository: testOwner + "/" + testRepo, Tag: testTag, Pipeline: &Pipeline{ID: "p1"}}
	start := time.Now()
	err = RecordPublishStatus(ctx, c, result, ciFailedErr("go-build failed"))
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), time.Second)
	require.Zero(t, calls)
}
