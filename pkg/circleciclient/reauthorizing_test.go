package circleciclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type renewalTransport func(*http.Request) (*http.Response, error)

func (f renewalTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func renewalResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCircleCIReadRenewal(t *testing.T) {
	for _, tc := range []struct {
		name       string
		next       string
		status     int
		persistent bool
	}{
		{name: "renewed login", next: "fixture-new", status: 401},
		{name: "transient refusal without a new login", next: "fixture-old", status: 401},
		{name: "persistent refusal", next: "fixture-new", status: 401, persistent: true},
		{name: "forbidden is not retried", next: "fixture-new", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var tokens []string
				var requests []time.Time
				renewals := 0
				client, err := New(Config{Token: "fixture-old", Transport: renewalTransport(func(r *http.Request) (*http.Response, error) {
					tokens = append(tokens, r.Header.Get("Circle-Token"))
					requests = append(requests, time.Now())
					if len(tokens) == 1 || tc.persistent {
						return renewalResponse(tc.status, `{"message":"refused"}`), nil
					}
					return renewalResponse(200, `{"slug":"gh/o/r"}`), nil
				}), Renew: func(_ context.Context, rejected string) (string, error) {
					renewals++
					if rejected != "fixture-old" {
						t.Fatal("renewal did not name the refused token")
					}
					return tc.next, nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				_, err = client.GetProject(t.Context(), "o", "r")
				if tc.persistent || tc.status != 401 {
					if agentcli.Exit(err) != agentcli.ExitRemoteError {
						t.Fatalf("remote refusal: %v", err)
					}
					if tc.status == 401 && !IsAPI(err) || tc.status == 403 && !IsForbidden(err) {
						t.Fatal("remote wrapper lost the API error kind")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if _, err := client.GetProject(t.Context(), "o", "r"); err != nil {
						t.Fatal(err)
					}
				}
				want := []string{"fixture-old"}
				if tc.status == 401 {
					want = append(want, tc.next)
					if renewals != 1 || requests[1].Sub(requests[0]) != 2*time.Second {
						t.Fatalf("want one renewal and a two-second pause; renewals=%d", renewals)
					}
					if !tc.persistent {
						want = append(want, tc.next)
					}
				} else if renewals != 0 {
					t.Fatal("renewed a non-401 failure")
				}
				if !reflect.DeepEqual(tokens, want) {
					t.Fatal("requests did not retain the selected credential")
				}
			})
		})
	}
}

func TestCircleCIRenewalBoundaries(t *testing.T) {
	for _, tc := range []string{"write", "fixed credential", "renewal failure", "cancel pause", "5xx"} {
		t.Run(tc, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls, renewals := 0, 0
				failure := errors.New("fixture source unavailable")
				config := Config{Token: "fixture-old", Transport: renewalTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if tc == "cancel pause" {
						go func() { time.Sleep(time.Second); cancel() }()
					}
					status := 401
					if tc == "5xx" {
						status = 503
					}
					return renewalResponse(status, `{"message":"refused"}`), nil
				}), Renew: func(context.Context, string) (string, error) {
					renewals++
					return "", failure
				}}
				if tc == "fixed credential" {
					config.Renew = nil
				}
				if tc == "5xx" {
					config.Transport = &agentcli.Retrying{Base: config.Transport, Attempts: 2}
				}
				client, err := New(config)
				if err != nil {
					t.Fatal(err)
				}
				method := http.MethodGet
				if tc == "write" {
					method = http.MethodPost
				}
				err = client.do(ctx, method, "/api/v2/me", nil, nil)
				wantExit := agentcli.ExitRemoteError
				if tc == "cancel pause" {
					wantExit = agentcli.ExitUsage // a cancelled read is the caller's outcome
				}
				if tc == "5xx" {
					wantExit = agentcli.ExitTimeout // an exhausted read is transient
				}
				if agentcli.Exit(err) != wantExit {
					t.Fatalf("want a remote error, got %v", err)
				}
				wantCalls, wantRenewals := 1, 0
				if tc == "5xx" {
					wantCalls = 2
					var exhausted *agentcli.RetriesExhaustedError
					if !errors.As(err, &exhausted) {
						t.Fatal("remote wrapper hid exhausted retries from the waiter")
					}
				}
				if tc == "renewal failure" {
					wantRenewals = 1
					if !errors.Is(err, failure) {
						t.Fatal("lost renewal error")
					}
				}
				if tc == "cancel pause" && !errors.Is(err, context.Canceled) {
					t.Fatal("lost cancellation")
				}
				if calls != wantCalls || renewals != wantRenewals {
					t.Fatalf("calls=%d renewals=%d", calls, renewals)
				}
			})
		})
	}
}

func TestCircleCIRenewalKeepsOutputAnonymous(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client, err := New(Config{Token: "fixture-old", Transport: renewalTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.Host == "output.invalid" {
				if req.Header.Get("Circle-Token") != "" || req.Header.Get("Authorization") != "" {
					t.Fatal("signed output received an API credential")
				}
				return renewalResponse(200, `[{"message":"step output"}]`), nil
			}
			if req.Header.Get("Circle-Token") == "fixture-old" {
				return renewalResponse(401, `{"message":"refused"}`), nil
			}
			return renewalResponse(200, `{"slug":"gh/o/r"}`), nil
		}), Renew: func(context.Context, string) (string, error) { return "fixture-new", nil }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.GetProject(t.Context(), "o", "r"); err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := client.stepOutput(t.Context(), "https://output.invalid/step", &out); err != nil {
			t.Fatal(err)
		}
		if calls != 3 || out.String() != "step output" {
			t.Fatalf("calls=%d output=%q", calls, out.String())
		}
	})
}
