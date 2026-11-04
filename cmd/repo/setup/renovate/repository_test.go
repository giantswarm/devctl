package renovate

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
)

// Malformed slugs must fail before authentication or HTTP: URL delimiters
// and escapes would otherwise change the repository go-github looks up.
func TestRunRejectsInvalidRepository(t *testing.T) {
	for _, slug := range []string{
		"", "repo", "/repo", "giantswarm/", "/", "giantswarm/repo/extra",
		"giantswarm/repo?other", "giantswarm/repo#other", "giantswarm/%72epo",
		"giantswarm/.", "giantswarm/..", "giantswarm/repo name",
	} {
		for _, remove := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/remove=%t", slug, remove), func(t *testing.T) {
				requests := 0
				command, _ := newTestCommand(t, func(req *http.Request) (*http.Response, error) {
					requests++
					return testResponse(req, http.StatusOK, `{"id":42,"name":"repo"}`), nil
				})
				args := []string{"renovate", slug}
				if remove {
					args = append(args, "--remove")
				}
				command.SetArgs(args)
				if err := command.Execute(); !IsInvalidArg(err) {
					t.Errorf("want invalidArgError, got %v", err)
				}
				if requests != 0 {
					t.Errorf("invalid slug sent %d HTTP requests", requests)
				}
			})
		}
	}
}

// Keep legitimate mixed-case names and dots/underscores working, and use the
// repository ID returned by the read for both permission operations.
func TestRunChangesPermissions(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprintf("remove=%t", remove), func(t *testing.T) {
			var requests []string
			command, _ := newTestCommand(t, func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.Method+" "+req.URL.Path)
				if req.Header.Get("Authorization") != "Bearer offline-test-token" {
					t.Error("request did not use the inherited token flag")
				}
				if req.Method == http.MethodGet {
					return testResponse(req, http.StatusOK, `{"id":42,"name":"Repo.Name_1-2"}`), nil
				}
				return testResponse(req, http.StatusNoContent, ""), nil
			})
			args := []string{"renovate", "GiantSwarm/Repo.Name_1-2"}
			method := http.MethodPut
			if remove {
				args = append(args, "--remove")
				method = http.MethodDelete
			}
			command.SetArgs(args)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			want := []string{
				"GET /repos/GiantSwarm/Repo.Name_1-2",
				method + " /user/installations/17164699/repositories/42",
			}
			if !slices.Equal(requests, want) {
				t.Errorf("requests = %v, want %v", requests, want)
			}
		})
	}
}
