package login

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/muster"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// The manager's client relies on the caller's context for its deadline. A
// stalled initialize request must consume the sign-in budget, too, before
// core_auth_login or the get_info polling can begin.
func TestManagerSignInBoundsInitialRequest(t *testing.T) {
	clock := agentcli.NewVirtualClock(time.Now())
	var canceled bool
	previous := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Advance the sign-in clock as if the server stalled for its entire
		// budget. No network request or wall-clock wait is made.
		_ = clock.Sleep(req.Context(), managerSignInTimeout)
		canceled = errors.Is(req.Context().Err(), context.Canceled) || errors.Is(req.Context().Err(), context.DeadlineExceeded)
		return nil, errors.New("synthetic stalled initialize")
	})}
	t.Cleanup(func() { http.DefaultClient = previous })
	r := &runner{clock: clock, stderr: io.Discard}
	_, err := r.signInManager(context.Background(), authstore.Token{
		Endpoint: "http://muster.invalid/mcp", Value: "synthetic-token",
	}, agentcli.NewProgress(io.Discard, false))
	if err == nil || !canceled {
		t.Fatalf("initial manager request exceeded the sign-in budget: canceled=%v err=%v", canceled, err)
	}
}

func TestManagerSignInSharesDeadlineWithPolling(t *testing.T) {
	const token = "synthetic-token"
	m := muster.Start(muster.Config{Bearer: token, Tools: map[string][]muster.ToolResponse{
		manager.ToolAuthLogin: {{Result: "Already signed in"}},
		manager.ToolGetInfo:   {{Error: "manager still requires authentication"}},
	}})
	defer m.Close()
	start := time.Now()
	clock := agentcli.NewVirtualClock(start)
	previous := http.DefaultClient
	initial := true
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if initial {
			initial = false
			// Most of the budget passes before the initial handshake answers.
			_ = clock.Sleep(req.Context(), 9*time.Minute)
		}
		return http.DefaultTransport.RoundTrip(req)
	})}
	t.Cleanup(func() { http.DefaultClient = previous })
	r := &runner{clock: clock, stderr: io.Discard}
	_, err := r.signInManager(context.Background(), authstore.Token{
		Endpoint: m.MCPURL(), Value: token,
	}, agentcli.NewProgress(io.Discard, false))
	if err == nil {
		t.Fatal("manager consent unexpectedly succeeded")
	}
	if elapsed := clock.Now().Sub(start); elapsed != managerSignInTimeout {
		t.Errorf("manager sign-in took %s, want a total budget of %s", elapsed, managerSignInTimeout)
	}
}

func TestManagerSignInHonorsParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previous := http.DefaultClient
	var canceled bool
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		canceled = errors.Is(req.Context().Err(), context.Canceled)
		return nil, req.Context().Err()
	})}
	t.Cleanup(func() { http.DefaultClient = previous })
	r := &runner{clock: agentcli.NewClock(1, nil), stderr: io.Discard}
	_, err := r.signInManager(ctx, authstore.Token{
		Endpoint: "http://muster.invalid/mcp", Value: "synthetic-token",
	}, agentcli.NewProgress(io.Discard, false))
	if err == nil || !canceled {
		t.Fatalf("initial manager request ignored parent cancellation: canceled=%v err=%v", canceled, err)
	}
}
