package setup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

func TestSetupPrerequisiteLookup(t *testing.T) {
	for _, status := range []int{503, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			r, output := setupTestRunner(t)
			reads := 0
			client := setupTestClient(t, false, func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "GET /repos/example/tool", req.Method+" "+req.URL.Path, "no other call can follow a missing prerequisite")
				reads++
				if reads == 1 {
					return setupAPIResponse(req, 200, setupRepositoryJSON("main", false)), nil
				}
				return setupAPIResponse(req, status, `{"message":"lookup unavailable"}`), nil
			})
			err := r.runWithClient(t.Context(), "example", "tool", client)
			if status == 503 {
				require.ErrorContains(t, err, "lookup unavailable")
			} else {
				require.NoError(t, err, "a missing-repository finding keeps the engine's finding exit policy")
			}
			var result reconcile.Result
			require.NoError(t, json.Unmarshal(output.Bytes(), &result))
			require.False(t, result.Converged)
			lookup := result.Step(reconcile.StepCreate)
			require.NotNil(t, lookup, "the failed prerequisite must survive step selection")
			if status == 503 {
				require.Equal(t, reconcile.VerdictFailed, lookup.Verdict)
				require.Len(t, result.Failed(), 1)
			} else {
				require.Equal(t, reconcile.FindingRepositoryMissing, lookup.Findings[0].Kind)
			}
			require.Equal(t, 2, reads)
		})
	}
}

func TestRestrictedSuccessfulLookupStaysHidden(t *testing.T) {
	client := setupTestClient(t, false, func(req *http.Request) (*http.Response, error) {
		switch req.Method + " " + req.URL.Path {
		case "GET /repos/example/tool":
			return setupAPIResponse(req, 200, setupRepositoryJSON("main", false)), nil
		case "GET /repos/example/tool/teams":
			return setupAPIResponse(req, 200, `[]`), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
	})
	baseline := reconcile.DefaultBaseline()
	baseline.TeamPermissions = nil
	engine := reconcile.Runner{GitHub: client.GetUnderlyingClient(t.Context()), Baseline: &baseline}
	result, err := engine.Run(t.Context(), reconcile.Request{Owner: "example", Entry: reposetup.UndeclaredEntry(reposetup.Undeclared{Name: "tool"}), Mode: reconcile.ModeRepair, Steps: []reconcile.Step{reconcile.StepPermissions}})
	require.NoError(t, err)
	require.True(t, result.Converged)
	require.Nil(t, result.Step(reconcile.StepCreate))
	require.Len(t, result.Steps, 1)
}
