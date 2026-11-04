package setup

import (
	"context"
	"encoding/base64"
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

type setupTransport func(*http.Request) (*http.Response, error)

func (f setupTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func setupResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

type setupReportedChecks []string

func (c setupReportedChecks) ReportedChecks(_ context.Context, _ *github.Repository, _ string) ([]string, error) {
	return []string(c), nil
}

func TestSetupChecksSelection(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "an explicit list replaces the current and detected contexts",
			args: []string{"--checks=run-tests"},
			want: []string{"run-tests"},
		},
		{
			name: "an empty list removes every required context",
			args: []string{"--checks="},
		},
		{
			name: "explicit checks override the filter",
			args: []string{"--checks=validate-changelog", "--checks-filter=["},
			want: []string{"validate-changelog"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupFlags(t, tc.args...)
			require.NoError(t, f.Validate())
			baseline := (&runner{flag: f}).baseline()
			var written bool
			var required []string
			transport := setupTransport(func(req *http.Request) (*http.Response, error) {
				key := req.Method + " " + req.URL.Path
				switch key {
				case "GET /repos/example/tool":
					return setupResponse(req, http.StatusOK, `{"name":"tool","full_name":"example/tool","owner":{"login":"example"},"default_branch":"main"}`), nil
				case "GET /repos/example/tool/rulesets":
					return setupResponse(req, http.StatusOK, `[]`), nil
				case "GET /repos/example/tool/branches/main/protection":
					return setupResponse(req, http.StatusOK, `{"required_pull_request_reviews":{"required_approving_review_count":1},"enforce_admins":{"enabled":true},"required_status_checks":{"strict":false,"checks":[{"context":"legacy-ci"},{"context":"check-values-schema / validate"}]}}`), nil
				case "GET /repos/example/tool/contents/.circleci/workflows.yml":
					content := base64.StdEncoding.EncodeToString([]byte("workflows:\n  build:\n    jobs: [go-build]\n"))
					body := fmt.Sprintf(`{"type":"file","encoding":"base64","content":%q}`, content)
					return setupResponse(req, http.StatusOK, body), nil
				case "GET /repos/example/tool/contents/.circleci/custom.yml", "GET /repos/example/tool/contents/tests/e2e":
					return setupResponse(req, http.StatusNotFound, `{"message":"Not Found"}`), nil
				case "PUT /repos/example/tool/branches/main/protection":
					var protection github.ProtectionRequest
					if err := json.NewDecoder(req.Body).Decode(&protection); err != nil {
						return nil, err
					}
					written = true
					if checks := protection.RequiredStatusChecks; checks != nil && checks.Checks != nil {
						for _, check := range *checks.Checks {
							required = append(required, check.Context)
						}
					}
					return setupResponse(req, http.StatusOK, `{}`), nil
				default:
					return nil, fmt.Errorf("unexpected request: %s", key)
				}
			})
			gh, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: transport}))
			require.NoError(t, err)
			r := reconcile.Runner{
				GitHub:   gh,
				Checks:   setupReportedChecks{"legacy-ci", "pre-commit", "check-values-schema / validate", "ci/circleci: go-build"},
				Baseline: &baseline,
			}
			res, err := r.Run(t.Context(), reconcile.Request{
				Owner: "example",
				Entry: reposetup.UndeclaredEntry(reposetup.Undeclared{Name: "tool"}),
				Mode:  reconcile.ModeRepair,
				Steps: []reconcile.Step{reconcile.StepProtection},
			})
			require.NoError(t, err)
			require.Empty(t, res.Failed())
			require.True(t, written, "the real protection step must write its selected contexts")
			require.ElementsMatch(t, tc.want, required)
		})
	}
}
