package setup

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetupDisableRulesetProtection(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		rule                             string
		classic, dryRun, paginated, fail bool
	}{
		{name: "owned ruleset", rule: `{"type":"pull_request","ruleset_id":1,"ruleset_source_type":"Repository","ruleset_source":"example/tool"}`},
		{name: "mixed protection", rule: `{"type":"required_status_checks","ruleset_id":2}`, classic: true},
		{name: "inherited organization rule", rule: `{"type":"non_fast_forward","ruleset_id":3,"ruleset_source_type":"Organization","ruleset_source":"example"}`},
		{name: "line gate", rule: `{"type":"required_status_checks","ruleset_id":4}`},
		{name: "dry run still reports remaining protection", rule: `{"type":"pull_request","ruleset_id":1}`, dryRun: true},
		{name: "later page rule", rule: `{"type":"deletion","ruleset_id":5}`, paginated: true},
		{name: "later page failure", paginated: true, fail: true},
		{name: "classic only", classic: true},
		{name: "disabled or unrelated rules do not apply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--disable-branch-protection"}
			if tc.dryRun {
				args = append(args, "--dry-run")
			}
			r, _ := setupTestRunner(t, args...)
			reads, removals, writes := 0, 0, 0
			transport := setupAPITransport(func(req *http.Request) (*http.Response, error) {
				key := req.Method + " " + req.URL.Path
				if req.Method != "GET" {
					writes++
				}
				switch key {
				case "GET /repos/example/tool":
					return setupAPIResponse(req, 200, setupRepositoryJSON("main", false)), nil
				case "GET /repos/example/tool/rules/branches/main":
					reads++
					if tc.paginated && req.URL.Query().Get("page") == "" {
						res := setupAPIResponse(req, 200, `[]`)
						res.Header.Set("Link", `<https://api.github.com/repos/example/tool/rules/branches/main?page=2>; rel="next"`)
						return res, nil
					}
					if tc.fail {
						return setupAPIResponse(req, 503, `{"message":"rules unavailable"}`), nil
					}
					body := `[]`
					if tc.rule != "" {
						body = "[" + tc.rule + "]"
					}
					return setupAPIResponse(req, 200, body), nil
				case "GET /repos/example/tool/branches/main/protection":
					if tc.classic {
						return setupAPIResponse(req, 200, `{}`), nil
					}
					return setupAPIResponse(req, 404, `{"message":"Branch not protected"}`), nil
				case "DELETE /repos/example/tool/branches/main/protection":
					removals++
					return setupAPIResponse(req, 204, ``), nil
				case "GET /repos/example/tool/actions/permissions/workflow":
					return setupAPIResponse(req, 200, `{"default_workflow_permissions":"write"}`), nil
				case "GET /repos/example/tool/teams":
					return setupAPIResponse(req, 200, `[{"slug":"employees","permission":"admin"},{"slug":"bots","permission":"push"}]`), nil
				}
				return nil, fmt.Errorf("unexpected request: %s", key)
			})
			err := r.runWithClient(t.Context(), "example", "tool", setupTestClient(t, tc.dryRun, transport))
			if tc.fail {
				require.ErrorContains(t, err, "rules unavailable")
			} else if tc.rule != "" {
				require.ErrorContains(t, err, "cannot disable active ruleset protection")
			} else {
				require.NoError(t, err)
			}
			if tc.fail || tc.rule != "" {
				require.Zero(t, writes)
				require.Zero(t, removals)
			} else if tc.classic {
				require.Equal(t, 1, removals)
				require.Equal(t, 1, writes)
			} else {
				require.Zero(t, writes)
			}
			if tc.paginated {
				require.Equal(t, 2, reads)
			} else {
				require.Equal(t, 1, reads)
			}
		})
	}
}

func TestSetupDisableChecksDestinationRules(t *testing.T) {
	var branches []string
	client := setupTestClient(t, false, func(req *http.Request) (*http.Response, error) {
		branches = append(branches, req.URL.Path)
		if req.URL.Path == "/repos/example/tool/rules/branches/main" {
			return setupAPIResponse(req, 200, `[{"type":"deletion","ruleset_id":9}]`), nil
		}
		return setupAPIResponse(req, 200, `[]`), nil
	})
	require.ErrorContains(t, checkRulesetDisable(t.Context(), client, "example", "tool", "master", "main"), `"main"`)
	require.Equal(t, []string{"/repos/example/tool/rules/branches/master", "/repos/example/tool/rules/branches/main"}, branches)
}
