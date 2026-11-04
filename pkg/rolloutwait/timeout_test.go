package rolloutwait

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type rolloutRoundTripFunc func(*http.Request) (*http.Response, error)

func (f rolloutRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRolloutTimeoutBoundsClusterReads(t *testing.T) {
	for _, retry := range []bool{false, true} {
		name := "in flight"
		if retry {
			name = "retrying"
		}
		t.Run(name, func(t *testing.T) {
			clock := agentcli.NewVirtualClock(time.Now())
			calls := 0
			transport := rolloutRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if retry {
					return &http.Response{
						StatusCode: http.StatusServiceUnavailable,
						Status:     "503 Service Unavailable",
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("unavailable")),
					}, nil
				}
				if err := clock.Sleep(req.Context(), 10*time.Second); err != nil {
					return nil, err
				}
				return nil, errors.New("the cluster read outlived the rollout timeout")
			})
			client, err := NewClient(&rest.Config{Host: "https://cluster.example"}, func(http.RoundTripper) http.RoundTripper {
				if retry {
					return &agentcli.Retrying{Base: transport, Clock: clock}
				}
				return transport
			})
			if err != nil {
				t.Fatal(err)
			}
			w, err := New(Config{
				Installation: "myinstallation", Version: "v0.48.1", Charts: []string{chart},
				Client: client, Timeout: 10 * time.Second, Clock: clock,
			})
			if err != nil {
				t.Fatal(err)
			}
			result := NewResult("myinstallation", "")
			err = w.Wait(context.Background(), &result)
			if agentcli.Exit(err) != agentcli.ExitTimeout {
				t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitTimeout, agentcli.Exit(err), err)
			}
			if _, verdict := agentcli.Outcome(err); verdict != agentcli.VerdictTimeout {
				t.Errorf("verdict: %s", verdict)
			}
			if calls == 0 || (retry && calls >= agentcli.RetryAttempts) {
				t.Errorf("the timeout should end the request before retries are exhausted: %d calls", calls)
			}
		})
	}
}

func TestRolloutTimeoutBoundsReconcileAndKeepsLastPoll(t *testing.T) {
	clock := agentcli.NewVirtualClock(time.Now())
	patches := 0
	transport := rolloutRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPatch {
			patches++
			if err := clock.Sleep(req.Context(), 10*time.Second); err != nil {
				return nil, err
			}
			return nil, errors.New("the reconcile write outlived the rollout timeout")
		}
		resource := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
		items := []any{}
		switch resource {
		case "ocirepositories":
			items = append(items, ociRepository(map[string]any{"semver": ">=0.17.0"}).Object)
		case "helmreleases":
			items = append(items, helmRelease("0.48.0", nil).Object)
		case "helmcharts", "apps":
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		body, err := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "List", "items": items})
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK",
			Header: http.Header{"Content-Type": []string{"application/json"}},
			Body:   io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})
	client, err := NewClient(&rest.Config{Host: "https://cluster.example"}, func(http.RoundTripper) http.RoundTripper {
		return &agentcli.Retrying{Base: transport, Clock: clock}
	})
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(Config{
		Installation: "myinstallation", Version: "v0.48.1", Charts: []string{chart},
		Client: client, Timeout: 10 * time.Second, Clock: clock, Reconcile: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := NewResult("myinstallation", "")
	err = w.Wait(context.Background(), &result)
	if agentcli.Exit(err) != agentcli.ExitTimeout {
		t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitTimeout, agentcli.Exit(err), err)
	}
	if patches != 1 {
		t.Errorf("the timed-out write must not be retried: %d patches", patches)
	}
	if len(result.Deployments) != 1 || result.Deployments[0].State != StateProgressing || !strings.Contains(err.Error(), "runs 0.48.0") {
		t.Errorf("the timeout must retain the last poll: %+v (%v)", result, err)
	}
}
