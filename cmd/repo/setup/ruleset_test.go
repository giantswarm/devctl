package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

type rulesetTransport func(*http.Request) (*http.Response, error)

func (f rulesetTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type noSetupChecks struct{}

func (noSetupChecks) ReportedChecks(context.Context, *github.Repository, string) ([]string, error) {
	return nil, nil
}

func TestSetupRulesetRepairRefusal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    reconcile.Mode
		pending bool
	}{
		{name: "repair refuses pending rules", mode: reconcile.ModeRepair, pending: true},
		{name: "check reports pending rules", mode: reconcile.ModeCheck, pending: true},
		{name: "aligned repair succeeds", mode: reconcile.ModeRepair},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			transport := rulesetTransport(func(req *http.Request) (*http.Response, error) {
				response := func(status int, body string) (*http.Response, error) {
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				}
				if req.Method != "GET" {
					writes++
					return nil, fmt.Errorf("unexpected write: %s", req.URL.Path)
				}
				switch req.URL.Path {
				case "/repos/example/tool":
					return response(200, `{"name":"tool","full_name":"example/tool","owner":{"login":"example"},"default_branch":"main"}`)
				case "/repos/example/tool/rulesets":
					return response(200, `[{"id":1,"name":"devctl: default branch","target":"branch","enforcement":"active"}]`)
				case "/repos/example/tool/rulesets/1":
					checks := ``
					if tc.pending {
						checks = `,{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":false,"required_status_checks":[{"context":"stale"}]}}`
					}
					return response(200, `{"id":1,"name":"devctl: default branch","target":"branch","enforcement":"active","conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},"bypass_actors":[{"actor_id":4242,"actor_type":"Integration","bypass_mode":"pull_request"}],"rules":[{"type":"deletion"},{"type":"non_fast_forward"},{"type":"pull_request","parameters":{"required_approving_review_count":1}}`+checks+`]}`)
				case "/repos/example/tool/branches/main/protection":
					return response(404, `{"message":"Branch not protected"}`)
				}
				if strings.HasPrefix(req.URL.Path, "/repos/example/tool/contents/") {
					return response(404, `{"message":"Not Found"}`)
				}
				return nil, fmt.Errorf("unexpected request: %s", req.URL.Path)
			})
			client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: transport}))
			require.NoError(t, err)
			baseline := reconcile.DefaultBaseline()
			baseline.RequiredChecksIfReported = nil
			engine := reconcile.Runner{GitHub: client, Checks: noSetupChecks{}, Baseline: &baseline}
			result, err := engine.Run(t.Context(), reconcile.Request{Owner: "example", Entry: reposetup.UndeclaredEntry(reposetup.Undeclared{Name: "tool"}), Mode: tc.mode, Pipeline: [][]byte{}, Steps: []reconcile.Step{reconcile.StepProtection}})
			require.NoError(t, err)
			require.Empty(t, result.Failed())
			output := &bytes.Buffer{}
			r := &runner{flag: &flag{Output: "json"}, stdout: output}
			err = r.report(result)
			if tc.pending && tc.mode == reconcile.ModeRepair {
				require.ErrorContains(t, err, "ruleset protection pending")
			} else {
				require.NoError(t, err)
			}
			var reported reconcile.Result
			require.NoError(t, json.Unmarshal(output.Bytes(), &reported))
			if tc.pending {
				require.False(t, reported.Converged)
				require.Contains(t, output.String(), "repo setup cannot write rulesets")
				require.Contains(t, output.String(), "devctl repo reconcile --team-file=")
			} else {
				require.True(t, reported.Converged)
			}
			require.Zero(t, writes, "the existing no-App-id gate must remain effective")
		})
	}
}
