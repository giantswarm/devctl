package prmerge

import (
	"errors"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// redE2E is a head whose go-build passed and whose e2e check run failed.
func redE2E() sequence.Routes {
	return sequence.Routes{
		"GET /repos/o/r/commits/abc123/check-runs": {{Body: map[string]any{"total_count": 2, "check_runs": []any{
			map[string]any{"id": 1, "name": "go-build", "status": "completed", "conclusion": "success", "html_url": "https://github.com/o/r/runs/1"},
			map[string]any{"id": 2, "name": "e2e", "status": "completed", "conclusion": "failure", "html_url": "https://github.com/o/r/runs/2"},
		}}}},
	}
}

// requires is a base whose branch protection requires contexts.
func requires(contexts ...string) sequence.Routes {
	list := make([]any, 0, len(contexts))
	for _, c := range contexts {
		list = append(list, c)
	}
	return sequence.Routes{"GET /repos/o/r/branches/main/protection": {{Body: map[string]any{
		"required_status_checks": map[string]any{"strict": false, "contexts": list},
	}}}}
}

func Test_Merge_acceptRed(t *testing.T) {
	tests := []struct {
		name       string
		routes     sequence.Routes
		acceptRed  []string
		wantCode   int
		wantReason string
		// wantWait: the head's checks were read.
		wantWait     bool
		wantAccepted []string
	}{
		{
			name:         "a red non-required check named: merged, recorded with its run",
			routes:       routes(pull(nil), redE2E()),
			acceptRed:    []string{"e2e"},
			wantWait:     true,
			wantAccepted: []string{"e2e https://github.com/o/r/runs/2"},
		},
		{
			name:       "a red check not named: red as without the flag",
			routes:     routes(pull(nil), redE2E()),
			wantCode:   1,
			wantReason: "check e2e concluded failure",
			wantWait:   true,
		},
		{
			name:       "another check named: the red one is still red",
			routes:     routes(pull(nil), redE2E()),
			acceptRed:  []string{"go-build"},
			wantCode:   1,
			wantReason: "check e2e concluded failure",
			wantWait:   true,
		},
		{
			name:       "a required check named: exit 3 before the wait",
			routes:     routes(pull(nil), redE2E(), requires("e2e")),
			acceptRed:  []string{"e2e"},
			wantCode:   3,
			wantReason: "--accept-red e2e: o/r requires it on main",
		},
		{
			name:       "a security scan named: exit 3 before the wait",
			routes:     routes(pull(nil), redE2E()),
			acceptRed:  []string{"e2e", "Gitleaks / scan"},
			wantCode:   3,
			wantReason: "--accept-red Gitleaks / scan: a security scan (gitleaks)",
		},
		{
			name:       "CodeQL named: exit 3 before the wait",
			routes:     routes(pull(nil), redE2E()),
			acceptRed:  []string{"Analyze (go) / CodeQL"},
			wantCode:   3,
			wantReason: "a security scan (codeql)",
		},
		{
			name:       "a check the head does not carry: exit 3 once green, nothing merged",
			routes:     routes(pull(nil)),
			acceptRed:  []string{"e2e"},
			wantCode:   3,
			wantReason: "--accept-red names e2e, and no check",
			wantWait:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.routes, func(c *Config) { c.Wait.AcceptRed = tc.acceptRed })
			result, err := h.merge(t)
			if code := exitCode(err); code != tc.wantCode {
				t.Fatalf("want exit %d, got %d: %v", tc.wantCode, code, err)
			}
			if waited := h.requested("GET /repos/o/r/commits/abc123/check-runs") > 0; waited != tc.wantWait {
				t.Errorf("want the head's checks read %t, got %t", tc.wantWait, waited)
			}
			var accepted []string
			for _, a := range result.AcceptedRed {
				accepted = append(accepted, a.Name+" "+a.URL)
			}
			if strings.Join(accepted, ",") != strings.Join(tc.wantAccepted, ",") {
				t.Errorf("want acceptedRed %v, got %v", tc.wantAccepted, accepted)
			}
			if tc.wantCode == 0 {
				if result.MergeCommitSHA != "m1" || h.requested("PUT /repos/o/r/pulls/42/merge") != 1 {
					t.Errorf("want a merge, got %+v", result)
				}
				if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Warnings, "\n"), "accepted red: check_run e2e concluded failure") {
					t.Errorf("want a warning naming the accepted check, got %v", result.Warnings)
				}
				return
			}
			var exitErr *agentcli.ExitError
			errors.As(err, &exitErr)
			if !strings.Contains(exitErr.Reason, tc.wantReason) {
				t.Errorf("want a reason with %q, got %q", tc.wantReason, exitErr.Reason)
			}
			if h.requested("PUT /repos/o/r/pulls/42/merge") != 0 || result.MergeCommitSHA != "" {
				t.Errorf("nothing merges on exit %d: %+v", tc.wantCode, result)
			}
		})
	}
}
