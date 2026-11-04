package merge

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	circlecimock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

func TestRunRecordsFailedPublishAfterMerge(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			routes := green()
			routes["GET /repos/o/r/releases/tags/v1.2.3"] = []sequence.Response{{Body: map[string]any{"id": 42, "body": "Keep notes"}}}
			routes["PATCH /repos/o/r/releases/42"] = []sequence.Response{{Status: status, Body: map[string]any{}}}
			r, stdout, _, ghServer := newRunner(t, routes, loggedIn, nil)
			cc, err := circlecimock.Start(sequence.Routes{
				"GET /api/v2/project/gh/o/r/pipeline": {{Body: map[string]any{"items": []any{map[string]any{"id": "p1", "number": 12, "vcs": map[string]any{"tag": "v1.2.3", "revision": "m1"}}}}}},
				"GET /api/v2/pipeline/p1/workflow":    {{Body: map[string]any{"items": []any{map[string]any{"id": "w1", "name": "build", "status": "failed"}}}}},
				"GET /api/v2/workflow/w1/job":         {{Body: map[string]any{"items": []any{map[string]any{"name": "go-build", "status": "failed", "type": "build"}}}}},
			})
			require.NoError(t, err)
			t.Cleanup(cc.Close)
			endpoints := r.endpoints()
			endpoints.CircleCIAPIURL = cc.APIURL()
			r.endpoints = func() agentcli.Endpoints { return endpoints }
			r.requireCircleCI = func(context.Context) (authstore.Token, error) { return authstore.Token{Value: "cci_test"}, nil }
			s, err := releasewaittest.GeneratedRelease("o", "r", &releasewaittest.Registry{}, "failed")
			require.NoError(t, err)
			s.GitHub.(*releasewaittest.GitHub).Tags["v1.2.3"] = "m1"
			r.github = func(config githubclient.Config) (clients, error) {
				c, err := newClients(config)
				c.release = s.GitHub
				c.entries = s.Entries
				return c, err
			}
			err = r.run(t.Context(), []string{"o/r", "42"})
			require.Equal(t, agentcli.ExitReleaseFailed, agentcli.Exit(err))
			doc := decode(t, stdout)
			require.Equal(t, "m1", doc["mergeCommitSha"])
			require.Equal(t, string(agentcli.VerdictReleaseFailed), doc["verdict"])
			patches := 0
			for _, request := range ghServer.Requests() {
				if request.Method == http.MethodPatch {
					patches++
					require.Equal(t, "/repos/o/r/releases/42", request.Path)
				}
			}
			require.Equal(t, 1, patches)
			warnings := doc["warnings"].([]any)
			if status == http.StatusForbidden {
				require.Len(t, warnings, 1)
				require.Contains(t, warnings[0], "could not record publish status")
			} else {
				require.Empty(t, warnings)
			}
		})
	}
}
