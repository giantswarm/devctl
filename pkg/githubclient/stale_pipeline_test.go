package githubclient

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

func TestStalePipelineEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, branch, head, prs string
		want                    bool
		fail                    bool
	}{
		{name: "default branch protected", branch: "main", head: "new"},
		{name: "current feature retained", branch: "feature", head: "old", prs: "[]"},
		{name: "superseded feature", branch: "feature", head: "new", prs: "[]", want: true},
		{name: "deleted unknown branch", branch: "feature", prs: "[]"},
		{name: "closed deleted branch", branch: "feature", prs: `[{"state":"closed","head":{"ref":"feature","sha":"old","repo":{"full_name":"o/r"}}}]`, want: true},
		{name: "reused branch without open PR retained", branch: "feature", head: "old", prs: `[{"state":"closed","head":{"ref":"feature","sha":"previous","repo":{"full_name":"o/r"}}}]`},
		{name: "closed current revision", branch: "feature", head: "old", prs: `[{"state":"closed","head":{"ref":"feature","sha":"old","repo":{"full_name":"o/r"}}}]`, want: true},
		{name: "open PR retains reused branch", branch: "feature", head: "old", prs: `[{"state":"closed","head":{"ref":"feature","sha":"old","repo":{"full_name":"o/r"}}},{"state":"open","head":{"ref":"feature","sha":"old","repo":{"full_name":"o/r"}}}]`},
		{name: "closed fork", branch: "pull/12", prs: `{"state":"closed","head":{"sha":"old"}}`, want: true},
		{name: "current fork", branch: "pull/12", prs: `{"state":"open","head":{"sha":"old"}}`},
		{name: "superseded fork", branch: "pull/12", prs: `{"state":"open","head":{"sha":"new"}}`, want: true},
		{name: "GitHub failure", branch: "feature", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v3/repos/o/r":
					_, _ = w.Write([]byte(`{"default_branch":"main"}`))
				case "/api/v3/repos/o/r/pulls", "/api/v3/repos/o/r/pulls/12":
					if tc.fail {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					_, _ = w.Write([]byte(tc.prs))
				case "/api/v3/repos/o/r/git/ref/heads/feature":
					if tc.head == "" {
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(`{"message":"not found"}`))
						return
					}
					_, _ = w.Write([]byte(`{"object":{"sha":"` + tc.head + `"}}`))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			c, err := New(Config{Logger: logrus.New(), AccessToken: "fixture", BaseURL: server.URL + "/"})
			require.NoError(t, err)
			p := circleciclient.Pipeline{}
			p.VCS.Branch, p.VCS.Revision = tc.branch, "old"
			got, err := c.StalePipeline(t.Context(), "o", "r", p)
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, got)
		})
	}
}
