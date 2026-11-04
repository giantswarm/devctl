package githubclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/sirupsen/logrus"
)

type permissionTransport func(*http.Request) (*http.Response, error)

func (f permissionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRenewOnAcceptedPermissions(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		accepted     string
		appBody      string
		appStatus    int
		rateHeader   string
		static       bool
		keepRefusing bool
		dryRun       bool
		enterprise   bool
		renewFails   bool
		wantProbes   int
		wantRenew    int
	}{
		{name: "case 0: 404 gains permission", status: 404, accepted: "repository_hooks=read", wantProbes: 1, wantRenew: 1},
		{name: "case 1: 403 gains permission", status: 403, accepted: "repository_hooks=write", wantProbes: 1, wantRenew: 1},
		{name: "case 2: retry refusal is final", status: 404, accepted: "repository_hooks=read", keepRefusing: true, wantProbes: 1, wantRenew: 1},
		{name: "case 3: app lacks permission", status: 404, accepted: "administration=write", wantProbes: 1},
		{name: "case 4: app only has read", status: 403, accepted: "contents=write", wantProbes: 1},
		{name: "case 5: entire set required", status: 404, accepted: "repository_hooks=read,administration=write", wantProbes: 1},
		{name: "case 6: alternative set held", status: 404, accepted: "administration=write; repository_hooks=read, contents=read", wantProbes: 1, wantRenew: 1},
		{name: "case 7: malformed header", status: 404, accepted: "repository_hooks=read,", wantProbes: 1},
		{name: "case 8: unknown access", status: 403, accepted: "repository_hooks=other", wantProbes: 1},
		{name: "case 9: no header", status: 404},
		{name: "case 10: explicit static token", status: 404, accepted: "repository_hooks=read", static: true},
		{name: "case 11: public record unavailable", status: 404, accepted: "repository_hooks=read", appStatus: 403, wantProbes: 1},
		{name: "case 12: invalid public record", status: 404, accepted: "repository_hooks=read", appBody: "{", wantProbes: 1},
		{name: "case 13: spent rate limit", status: 403, accepted: "repository_hooks=read", rateHeader: "X-RateLimit-Remaining"},
		{name: "case 14: secondary rate limit", status: 403, accepted: "repository_hooks=read", rateHeader: "Retry-After"},
		{name: "case 15: server error", status: 500, accepted: "repository_hooks=read"},
		{name: "case 16: dry run still renews reads", status: 404, accepted: "repository_hooks=read", dryRun: true, wantProbes: 1, wantRenew: 1},
		{name: "case 17: enterprise API prefix", status: 404, accepted: "repository_hooks=read", enterprise: true, wantProbes: 1, wantRenew: 1},
		{name: "case 18: renewal failure is returned", status: 404, accepted: "repository_hooks=read", renewFails: true, wantProbes: 1, wantRenew: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := "/custom/"
			if tc.enterprise {
				prefix = "/api/v3/"
			}
			var probes int
			var tokens []string
			transport := permissionTransport(func(req *http.Request) (*http.Response, error) {
				header := http.Header{"Content-Type": {"application/json"}}
				status, body := http.StatusOK, `[{"id":42}]`
				switch req.URL.Path {
				case prefix + "apps/giantswarm-devctl":
					probes++
					if req.Header.Get("Authorization") != "" {
						t.Error("public App lookup carried a credential")
					}
					if _, ok := req.Context().Deadline(); !ok {
						t.Error("public App lookup has no deadline")
					}
					body = `{"permissions":{"repository_hooks":"write","contents":"read"}}`
					if tc.appBody != "" {
						body = tc.appBody
					}
					if tc.appStatus != 0 {
						status = tc.appStatus
					}
				case prefix + "repos/o/r/hooks/1/deliveries":
					token := req.Header.Get("Authorization")
					tokens = append(tokens, token)
					if token != "Bearer ghu_fresh" || tc.keepRefusing {
						status, body = tc.status, `{"message":"original refusal"}`
						header.Set("X-Accepted-GitHub-Permissions", tc.accepted)
						if tc.rateHeader != "" {
							header.Set(tc.rateHeader, "0")
						}
					}
				default:
					return nil, errors.New("unexpected fixture request")
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			var logs bytes.Buffer
			logger := logrus.New()
			logger.SetOutput(&logs)
			logger.SetLevel(logrus.DebugLevel)
			renewalError := errors.New("fixture refresh refused")
			var renewals int
			config := Config{Logger: logger, AccessToken: "ghu_stale", BaseURL: "https://fixture.invalid/custom", Transport: transport, DryRun: tc.dryRun}
			if !tc.static {
				config.Renew = func(_ context.Context, rejected string) (string, error) {
					renewals++
					if rejected != "ghu_stale" {
						t.Error("renewal did not replace the refused token")
					}
					if tc.renewFails {
						return "", renewalError
					}
					return "ghu_fresh", nil
				}
			}
			var client *Client
			var err error
			if tc.enterprise || tc.dryRun {
				config.BaseURL = "https://fixture.invalid"
				prefix = "/api/v3/"
				client, err = New(config)
			} else {
				client, _, err = NewConditional(config)
			}
			if err != nil {
				t.Fatal(err)
			}
			deliveries, err := client.ListHookDeliveries(context.Background(), "o", "r", 1)
			wantTokens := []string{"Bearer ghu_stale"}
			if tc.wantRenew == 1 && !tc.renewFails {
				wantTokens = append(wantTokens, "Bearer ghu_fresh")
			}
			switch {
			case tc.renewFails:
				if !errors.Is(err, renewalError) {
					t.Fatalf("error = %v, want renewal refusal", err)
				}
			case tc.wantRenew == 1 && !tc.keepRefusing:
				if err != nil || len(deliveries) != 1 || deliveries[0].GetID() != 42 {
					t.Fatalf("deliveries = %v, error = %v", deliveries, err)
				}
				if _, err := client.ListHookDeliveries(context.Background(), "o", "r", 1); err != nil {
					t.Fatal(err)
				}
				wantTokens = append(wantTokens, "Bearer ghu_fresh")
			case tc.rateHeader == "X-RateLimit-Remaining":
				var refusal *github.RateLimitError
				if !errors.As(err, &refusal) || refusal.Response.StatusCode != tc.status || refusal.Message != "original refusal" {
					t.Fatalf("error = %v, want original rate-limit refusal", err)
				}
			default:
				var refusal *github.ErrorResponse
				if !errors.As(err, &refusal) || refusal.Response.StatusCode != tc.status || refusal.Message != "original refusal" {
					t.Fatalf("error = %v, want original HTTP %d refusal", err, tc.status)
				}
			}
			if probes != tc.wantProbes || renewals != tc.wantRenew || !reflect.DeepEqual(tokens, wantTokens) {
				t.Fatalf("probes = %d, renewals = %d, request sequence matches = %v; want %d, %d, true", probes, renewals, reflect.DeepEqual(tokens, wantTokens), tc.wantProbes, tc.wantRenew)
			}
			if tc.wantRenew == 1 && !tc.renewFails && !strings.Contains(logs.String(), "repository_hooks=") {
				t.Error("verbose output did not name the refreshed permission")
			}
			if strings.Contains(logs.String(), "ghu_") {
				t.Error("verbose output contains a fixture credential")
			}
		})
	}
}

func TestPermissionRenewalRequestBody(t *testing.T) {
	for _, replayable := range []bool{true, false} {
		t.Run(map[bool]string{true: "replayable", false: "streaming"}[replayable], func(t *testing.T) {
			var bodies []string
			var renewals, probes int
			transport := permissionTransport(func(req *http.Request) (*http.Response, error) {
				status, header, body := http.StatusOK, http.Header{}, `{"permissions":{"repository_hooks":"write"}}`
				if req.URL.Path == "/apps/giantswarm-devctl" {
					probes++
				} else {
					b, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatal(err)
					}
					_ = req.Body.Close()
					bodies = append(bodies, string(b))
					status = http.StatusNoContent
					if len(bodies) == 1 {
						status = http.StatusForbidden
						header.Set("X-Accepted-GitHub-Permissions", "repository_hooks=write")
					}
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			client, _, err := NewConditional(Config{Logger: logger, AccessToken: "ghu_stale", BaseURL: "https://fixture.invalid", Transport: transport, Renew: func(context.Context, string) (string, error) {
				renewals++
				return "ghu_fresh", nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://fixture.invalid/repos/o/r/hooks/1/deliveries/42/attempts", strings.NewReader(`{"value":"unchanged"}`))
			if err != nil {
				t.Fatal(err)
			}
			if !replayable {
				req.GetBody = nil
			}
			resp, err := client.ghClient.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			wantBodies, wantRenewals, wantStatus := []string{`{"value":"unchanged"}`}, 0, http.StatusForbidden
			if replayable {
				wantBodies = append(wantBodies, wantBodies[0])
				wantRenewals, wantStatus = 1, http.StatusNoContent
			}
			if !reflect.DeepEqual(bodies, wantBodies) || probes != wantRenewals || renewals != wantRenewals || resp.StatusCode != wantStatus {
				t.Fatalf("bodies = %q, probes = %d, renewals = %d, status = %d", bodies, probes, renewals, resp.StatusCode)
			}
			if req.Header.Get("Authorization") != "" {
				t.Error("renewal mutated the caller's request headers")
			}
		})
	}
}
