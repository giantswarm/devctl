package githubclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestEnsureReleasePublishStatus(t *testing.T) {
	for _, notes := range []string{"", "### Added\n\n- An image and chart.\n\n", "> A manually written notice.\n\nRelease notes without a final newline"} {
		t.Run(fmt.Sprintf("notes_%d", len(notes)), func(t *testing.T) {
			body := notes
			patches := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method + " " + r.URL.Path {
				case "GET /repos/o/r/releases/tags/v1.2.3":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "tag_name": "v1.2.3", "name": "Keep title", "body": body, "draft": false, "prerelease": true})
				case "PATCH /repos/o/r/releases/42":
					patches++
					var patch map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&patch))
					require.Len(t, patch, 1, "only release notes may change")
					body = patch["body"].(string)
					_, _ = io.WriteString(w, `{}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			c := publishClient(t, server.URL)
			pipeline := "https://app.circleci.com/pipelines/github/o/r/12"
			require.NoError(t, c.EnsureReleasePublishStatus(t.Context(), "o", "r", "v1.2.3", pipeline, true))
			require.Contains(t, body, "**Not published:**")
			require.Contains(t, body, "[tag pipeline]("+pipeline+")")
			require.True(t, strings.HasSuffix(body, notes))
			require.True(t, hasPublishFailure(body))
			marked := body
			require.NoError(t, c.EnsureReleasePublishStatus(t.Context(), "o", "r", "v1.2.3", pipeline, true))
			require.Equal(t, 1, patches, "repeated waits must not duplicate the notice or write again")
			require.Equal(t, marked, body)

			// A human's additions around the notice survive both a new failed
			// pipeline and its eventual recovery, byte for byte.
			body = "Human prefix.\n\n" + body + "\nHuman addition.\n"
			require.True(t, hasPublishFailure(body), "a human prefix must not hide the recorded failure")
			require.NoError(t, c.EnsureReleasePublishStatus(t.Context(), "o", "r", "v1.2.3", pipeline+"3", true))
			require.Equal(t, 1, strings.Count(body, publishFailureStart))
			require.Contains(t, body, pipeline+"3")
			require.NoError(t, c.EnsureReleasePublishStatus(t.Context(), "o", "r", "v1.2.3", pipeline+"3", false))
			require.Equal(t, "Human prefix.\n\n"+notes+"\nHuman addition.\n", body)
			require.False(t, hasPublishFailure(body))
			require.NoError(t, c.EnsureReleasePublishStatus(t.Context(), "o", "r", "v1.2.3", pipeline+"3", false))
			require.Equal(t, 3, patches)
		})
	}
}

func publishClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	c, _, err := NewConditional(Config{Logger: logger, AccessToken: "ghu_test", BaseURL: baseURL})
	require.NoError(t, err)
	return c
}

func TestEnsureReleasePublishStatusFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		failed  bool
		wantErr bool
	}{
		{name: "missing failed release", status: http.StatusNotFound, failed: true, wantErr: true},
		{name: "missing recovered release", status: http.StatusNotFound},
		{name: "forbidden read", status: http.StatusForbidden, failed: true, wantErr: true},
		{name: "incomplete notice", status: http.StatusOK, body: `{"id":42,"body":"<!-- devctl:publish-failed -->\nAn interrupted notice"}`, failed: true, wantErr: true},
		{name: "no release id", status: http.StatusOK, body: `{"body":"Keep notes"}`, failed: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, got := newWireClient(t, tc.status, tc.body)
			err := c.EnsureReleasePublishStatus(t.Context(), "o", "r", "v1.2.3", "pipeline", tc.failed)
			require.Equal(t, tc.wantErr, err != nil)
			require.Equal(t, http.MethodGet, got.method, "must not write after an incomplete read")
		})
	}
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			patches := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, `{"id":42,"body":"Keep notes"}`)
				} else {
					patches++
					w.WriteHeader(status)
				}
			}))
			t.Cleanup(server.Close)
			err := publishClient(t, server.URL).EnsureReleasePublishStatus(t.Context(), "o", "r", "v1.2.3", "pipeline", true)
			require.Error(t, err)
			require.Equal(t, 1, patches, "a failed write must not be retried")
		})
	}
}

func TestReleasePublishFailureMappingAndPagination(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "" && r.URL.Path == "/repos/o/r/releases" {
			w.Header().Set("Link", fmt.Sprintf("<http://%s/repos/o/r/releases?page=2&per_page=100>; rel=\"next\"", r.Host))
			_, _ = io.WriteString(w, `[{"tag_name":"v1.2.4","body":"No binaries: this release ships charts."}]`)
			return
		}
		rel := map[string]any{"tag_name": "v1.2.3", "html_url": "https://github.com/o/r/releases/tag/v1.2.3", "body": publishFailureStart + "\n> **Not published:** failed\n" + publishFailureEnd + "\n\nKeep notes"}
		if r.URL.Path == "/repos/o/r/releases" {
			_ = json.NewEncoder(w).Encode([]any{rel})
		} else {
			_ = json.NewEncoder(w).Encode(rel)
		}
	}))
	t.Cleanup(server.Close)
	c := publishClient(t, server.URL)
	releases, err := c.ListReleases(t.Context(), "o", "r")
	require.NoError(t, err)
	require.Len(t, releases, 2)
	require.False(t, releases[0].PublishFailed, "empty assets do not mean failed publication")
	require.True(t, releases[1].PublishFailed)
	byTag, err := c.GetReleaseByTag(t.Context(), "o", "r", "v1.2.3")
	require.NoError(t, err)
	require.True(t, byTag.PublishFailed)
	require.Equal(t, 3, requests)
}

func TestListReleasesRejectsIncompleteListing(t *testing.T) {
	for _, failPage := range []int{2, releasePages + 1} {
		t.Run(fmt.Sprint(failPage), func(t *testing.T) {
			pages := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pages++
				w.Header().Set("Content-Type", "application/json")
				if pages == failPage {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Header().Set("Link", fmt.Sprintf("<http://%s/repos/o/r/releases?page=%d>; rel=\"next\"", r.Host, pages+1))
				_, _ = io.WriteString(w, `[{"tag_name":"v1.2.3"}]`)
			}))
			t.Cleanup(server.Close)
			releases, err := publishClient(t, server.URL).ListReleases(t.Context(), "o", "r")
			require.Error(t, err)
			require.Nil(t, releases)
			require.Equal(t, min(failPage, releasePages), pages)
		})
	}
}
