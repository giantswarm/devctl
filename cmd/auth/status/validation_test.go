package status

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

type statusTransport func(*http.Request) (*http.Response, error)

func (f statusTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Status must only read these synthetic records, never refresh or discard them.
type statusReadStore struct {
	authstore.Store
	records map[string]authstore.Record
}

func (s statusReadStore) Get(user string) (authstore.Record, error) {
	record, ok := s.records[user]
	if !ok {
		return authstore.Record{}, authstore.ErrNotFound
	}
	return record, nil
}

func TestStatusValidatesCircleCI(t *testing.T) {
	const token = "synthetic-circleci-token" // #nosec G101 -- synthetic credential for the in-memory transport.
	testCases := []struct {
		name       string
		status     int
		body       string
		missing    bool
		expired    bool
		emptyToken bool
		noGitHub   bool
		wantExit   int
		wantReason string
		wantLogin  string
		wantHTTP   int
		invalid    bool
	}{
		{name: "case 0: rejected JSON", status: 401, body: `{"message":"` + token + `"}`, wantExit: 8, wantReason: "invalid (HTTP 401)", wantHTTP: 401, invalid: true},
		{name: "case 1: rejected plain text", status: 401, body: token, wantExit: 8, wantReason: "invalid (HTTP 401)", wantHTTP: 401, invalid: true},
		{name: "case 2: forbidden", status: 403, body: token, wantExit: 8, wantReason: "invalid (HTTP 403)", wantHTTP: 403, invalid: true},
		{name: "case 3: service failure", status: 500, body: token, wantExit: 7, wantReason: "HTTP 500", wantHTTP: 500},
		{name: "case 4: rate limited", status: 429, body: token, wantExit: 7, wantReason: "HTTP 429", wantHTTP: 429},
		{name: "case 5: malformed success", status: 200, body: token, wantExit: 7, wantReason: "HTTP 200", wantHTTP: 200},
		{name: "case 6: missing login", status: 200, body: `{}`, wantExit: 7, wantReason: "empty login"},
		{name: "case 7: accepted", status: 200, body: `{"login":"live-circleci"}`, wantLogin: "live-circleci"},
		{name: "case 8: absent", missing: true, wantExit: 8, wantReason: "no token in the keychain"},
		{name: "case 9: expired", expired: true, wantExit: 8, wantReason: "the token expired"},
		{name: "case 10: empty token", emptyToken: true, wantExit: 8, wantReason: "the token is invalid", invalid: true},
		{name: "case 11: check CircleCI even without GitHub", noGitHub: true, status: 200, body: `{"login":"live-circleci"}`, wantExit: 8, wantReason: "GitHub authentication required", wantLogin: "live-circleci"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			records := map[string]authstore.Record{
				authstore.UserGitHub: {Login: "github-user", Token: "synthetic-github-token", ExpiresAt: now.Add(-time.Hour),
					RefreshToken: "synthetic-refresh-token", RefreshExpiresAt: now.Add(time.Hour)},
				authstore.UserCircleCI: {Login: "stored-circleci", Token: token, ExpiresAt: now.Add(5 * 24 * time.Hour)},
				authstore.UserMuster:   {Login: "muster-user", Token: "synthetic-muster-token"},
			}
			if tc.missing {
				delete(records, authstore.UserCircleCI)
			}
			if tc.noGitHub {
				delete(records, authstore.UserGitHub)
			}
			if tc.expired || tc.emptyToken {
				record := records[authstore.UserCircleCI]
				if tc.expired {
					record.ExpiresAt = now.Add(-time.Hour)
				}
				if tc.emptyToken {
					record.Token = ""
				}
				records[authstore.UserCircleCI] = record
			}
			calls := 0
			r, stdout, stderr := newRunner(t, nil)
			r.open = func(w io.Writer) (*authstore.Auth, error) {
				return authstore.New(authstore.Config{
					Store:     statusReadStore{records: records},
					Clock:     agentcli.NewClock(1, func() time.Time { return now }),
					Endpoints: agentcli.Endpoints{CircleCIAPIURL: "https://circleci.invalid/api/v2"},
					HTTPClient: &http.Client{Transport: statusTransport(func(req *http.Request) (*http.Response, error) {
						calls++
						if req.Method != http.MethodGet || req.URL.String() != "https://circleci.invalid/api/v2/me" || req.Header.Get("Circle-Token") != token || req.Header.Get("Authorization") != "" {
							t.Error("validation must use GET /me with only the stored CircleCI credential")
						}
						return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
					})},
					Stderr: w,
				})
			}
			command := &cobra.Command{}
			command.SetContext(context.Background())
			err := r.Run(command, nil)
			if got := agentcli.Exit(err); got != tc.wantExit {
				t.Errorf("exit %d, want %d", got, tc.wantExit)
			}
			var doc document
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatalf("expected one JSON document: %v", err)
			}
			if doc.ExitCode != tc.wantExit || !strings.Contains(doc.Reason, tc.wantReason) {
				t.Errorf("envelope exit %d, reason %q; want %d, %q", doc.ExitCode, doc.Reason, tc.wantExit, tc.wantReason)
			}
			if doc.CircleCI.Invalid != tc.invalid || doc.CircleCI.HTTPStatus != tc.wantHTTP {
				t.Errorf("invalid %t, HTTP status %d; want %t, %d", doc.CircleCI.Invalid, doc.CircleCI.HTTPStatus, tc.invalid, tc.wantHTTP)
			}
			if tc.invalid && doc.CircleCI.Usable() {
				t.Error("rejected identity must not be usable")
			}
			if doc.CircleCI.Present != !tc.missing || doc.CircleCI.Expired != tc.expired {
				t.Error("validation changed the stored presence or expiry state")
			}
			if !tc.missing && !doc.CircleCI.ExpiresAt.Equal(records[authstore.UserCircleCI].ExpiresAt) {
				t.Error("validation lost the stored expiry")
			}
			if doc.Warnings == nil || doc.GitHub.Warnings == nil || doc.CircleCI.Warnings == nil || doc.Muster.Warnings == nil {
				t.Error("warning fields must remain arrays")
			}
			wantVerdict := agentcli.VerdictGreen
			switch tc.wantExit {
			case agentcli.ExitUsage:
				wantVerdict = agentcli.VerdictUsage
			case agentcli.ExitAuthRequired:
				wantVerdict = agentcli.VerdictAuthRequired
			}
			if doc.Verdict != wantVerdict || (tc.wantExit == 0 && doc.Reason != "") {
				t.Error("unexpected document verdict or success reason")
			}
			if tc.wantLogin != "" && doc.CircleCI.Login != tc.wantLogin {
				t.Errorf("login %q, want %q", doc.CircleCI.Login, tc.wantLogin)
			}
			if doc.Muster.Login != "muster-user" || (!tc.noGitHub && (!doc.GitHub.Expired || !doc.GitHub.Refreshable)) {
				t.Error("validation lost the other identities")
			}
			wantCalls := 1
			if tc.missing || tc.expired || tc.emptyToken {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Errorf("requests %d, want %d", calls, wantCalls)
			}
			if tc.invalid && !tc.noGitHub && !strings.Contains(doc.Reason, "devctl auth login --circleci-only") {
				t.Error("rejected token must name the CircleCI login command")
			}
			if stderr.Len() != 0 {
				t.Error("status without progress must leave stderr silent")
			}
			for _, value := range []string{token, "synthetic-github-token", "synthetic-refresh-token", "synthetic-muster-token"} {
				if strings.Contains(stdout.String()+stderr.String(), value) {
					t.Error("output contains synthetic credential material")
				}
			}
		})
	}
}

func TestStatusValidationUsesCommandContext(t *testing.T) {
	r, stdout, _ := newRunner(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := &cobra.Command{}
	command.SetContext(ctx)
	r.open = func(w io.Writer) (*authstore.Auth, error) {
		return authstore.New(authstore.Config{
			Store: statusReadStore{records: map[string]authstore.Record{
				authstore.UserGitHub:   {Token: "synthetic-github"},
				authstore.UserCircleCI: {Token: "synthetic-circleci"},
			}},
			Endpoints: agentcli.Endpoints{CircleCIAPIURL: "https://circleci.invalid/api/v2"},
			HTTPClient: &http.Client{Transport: statusTransport(func(req *http.Request) (*http.Response, error) {
				if req.Context().Err() != context.Canceled {
					t.Error("CircleCI request lost the command's cancellation")
				}
				return nil, req.Context().Err()
			})},
			Stderr: w,
		})
	}
	if got := agentcli.Exit(r.Run(command, nil)); got != agentcli.ExitUsage {
		t.Errorf("exit %d, want tooling failure", got)
	}
	var doc document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Reason, "context canceled") {
		t.Errorf("reason %q lacks cancellation", doc.Reason)
	}
}
