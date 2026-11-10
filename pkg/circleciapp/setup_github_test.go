package circleciapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// Exercise release discovery through the production adapter before Setup can
// dispatch any publishing pipelines, including when a later page is unreadable.
func TestSetupDiscoversPublishedReleasePages(t *testing.T) {
	for _, failPageTwo := range []bool{false, true} {
		t.Run(fmt.Sprintf("page_two_fails=%t", failPageTwo), func(t *testing.T) {
			published := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
			draft := release("a/v9.0.0", published)
			draft.Draft = true
			prerelease := release("b/v9.0.0", published)
			prerelease.Prerelease = true
			var pages []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var response any
				switch r.URL.Path {
				case "/repos/giantswarm/sample":
					response = map[string]any{"id": 42, "default_branch": "main"}
				case "/repos/giantswarm/sample/contents/.circleci/config.yml":
					response = map[string]any{"type": "file", "content": "", "encoding": "base64"}
				case "/repos/giantswarm/sample/releases/latest":
					response = release("a/v1.0.0", published)
				case "/repos/giantswarm/sample/releases":
					page := r.URL.Query().Get("page")
					pages = append(pages, page)
					switch page {
					case "":
						w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2&per_page=100>; rel="next"`, r.Host, r.URL.Path))
						response = []*github.RepositoryRelease{release("a/v1.0.0", published), draft, prerelease}
					case "2":
						if failPageTwo {
							w.WriteHeader(http.StatusInternalServerError)
							response = map[string]string{"message": "release page unavailable"}
						} else {
							response = []*github.RepositoryRelease{release("b/v2.0.0", published), release("a/v1.2.0", published), release("a/v1.1.0", published)}
						}
					default:
						t.Errorf("unexpected release page %q", page)
						w.WriteHeader(http.StatusNotFound)
						return
					}
				case "/repos/giantswarm/sample/commits/main/statuses":
					response = []*github.RepoStatus{status("go-build", "success", "head", published)}
				case "/repos/giantswarm/sample/commits/a/v1.2.0/statuses", "/repos/giantswarm/sample/commits/b/v2.0.0/statuses":
					response = []*github.RepoStatus{}
				default:
					t.Errorf("unexpected GitHub request %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Errorf("encoding GitHub fixture: %v", err)
				}
			}))
			defer srv.Close()
			client, err := github.NewClient(github.WithHTTPClient(srv.Client()))
			require.NoError(t, err)
			baseURL, err := url.Parse(srv.URL + "/")
			require.NoError(t, err)
			client.BaseURL = baseURL
			ci := newFake(appDef())
			ci.triggers["app"] = []circleciclient.Trigger{{ID: "on"}}

			report, err := Setup(context.Background(), ci, NewGitHub(client), "giantswarm", "sample", Options{
				Now: func() time.Time { return published.Add(time.Hour) },
			})
			require.Equal(t, []string{"", "2"}, pages)
			if failPageTwo {
				require.ErrorContains(t, err, "reading the releases of giantswarm/sample")
				require.ErrorContains(t, err, "release page unavailable")
				require.Nil(t, report)
				require.Zero(t, ci.seq)
				require.Empty(t, ci.writes)
				return
			}
			require.NoError(t, err)
			require.Len(t, report.Runs, 2)
			require.Equal(t, circleciclient.Ref{Tag: "a/v1.2.0"}, report.Runs[0].Ref)
			require.Equal(t, circleciclient.Ref{Tag: "b/v2.0.0"}, report.Runs[1].Ref)
			require.Equal(t, 2, ci.seq)
		})
	}
}
