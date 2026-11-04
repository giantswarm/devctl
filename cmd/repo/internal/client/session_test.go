package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type sessionCallerFunc func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f sessionCallerFunc) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

// A bounded watch sequence crosses expiry without sleeping or opening a
// real store. A valid bearer retains its MCP session until it rotates.
func TestSessionRefreshesMusterBetweenCalls(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	expires := now.Add(time.Minute)
	token := authstore.Token{Value: "synthetic-old", Endpoint: "https://muster.example/mcp", Login: "alice", ExpiresAt: expires}
	providerCalls := 0
	var created []string
	var used []string
	key := struct{}{}
	ctx := context.WithValue(context.Background(), key, "parent")
	s, err := openMuster(ctx, func(callCtx context.Context) (authstore.Token, error) {
		require.Equal(t, "parent", callCtx.Value(key))
		providerCalls++
		if !now.Before(expires) {
			token.Value = "synthetic-fresh"
			token.ExpiresAt = now.Add(time.Hour)
		}
		return token, nil
	}, func(current authstore.Token) manager.Caller {
		created = append(created, current.Value)
		return sessionCallerFunc(func(callCtx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
			require.Equal(t, "parent", callCtx.Value(key))
			require.Equal(t, manager.ToolWatchRepository, tool)
			require.Equal(t, "my-service", args["repository"])
			require.True(t, now.Before(current.ExpiresAt), "an expired bearer must never reach the tool")
			used = append(used, current.Value)
			if current.Value == "synthetic-fresh" {
				return json.RawMessage(`{"ready":true}`), nil
			}
			return json.RawMessage(`{"ready":false,"pending":"setUp"}`), nil
		})
	})
	require.NoError(t, err)
	require.Equal(t, "alice", s.Login)
	for range 2 {
		payload, err := s.Call(ctx, manager.ToolWatchRepository, map[string]any{"repository": "my-service"})
		require.NoError(t, err)
		require.JSONEq(t, `{"ready":false,"pending":"setUp"}`, string(payload))
	}
	now = now.Add(90 * time.Second)
	payload, err := s.Call(ctx, manager.ToolWatchRepository, map[string]any{"repository": "my-service"})
	require.NoError(t, err)
	require.JSONEq(t, `{"ready":true}`, string(payload))
	require.Equal(t, []string{"synthetic-old", "synthetic-fresh"}, created)
	require.Equal(t, []string{"synthetic-old", "synthetic-old", "synthetic-fresh"}, used)
	require.Equal(t, 4, providerCalls)
}

func TestSessionRefreshFailure(t *testing.T) {
	for _, failure := range []error{
		errors.New("synthetic storage failure"),
		context.Canceled,
		&authstore.AuthRequiredError{Identity: "muster", Cause: "synthetic refresh expired", Hint: "devctl auth login --muster-only"},
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			providerCalls, toolCalls := 0, 0
			s, err := openMuster(context.Background(), func(context.Context) (authstore.Token, error) {
				providerCalls++
				if providerCalls > 1 {
					return authstore.Token{}, failure
				}
				return authstore.Token{Value: "synthetic", Endpoint: "https://muster.example/mcp"}, nil
			}, func(authstore.Token) manager.Caller {
				return sessionCallerFunc(func(context.Context, string, map[string]any) (json.RawMessage, error) {
					toolCalls++
					return nil, nil
				})
			})
			require.NoError(t, err)
			_, err = s.Call(context.Background(), manager.ToolUpdateRepository, nil)
			require.ErrorIs(t, err, failure)
			require.Zero(t, toolCalls, "a failed refresh must not send a write")
		})
	}
}

type sessionTransportFunc func(*http.Request) (*http.Response, error)

func (f sessionTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Even a refused write gets one attempt. Endpoint changes must rebuild the
// client and name the current endpoint in the login hint.
func TestSessionRotatedEndpointRefusal(t *testing.T) {
	token := authstore.Token{Value: "synthetic", Endpoint: "https://old.example/mcp"}
	requests, clients := 0, 0
	s, err := openMuster(context.Background(), func(context.Context) (authstore.Token, error) {
		return token, nil
	}, func(current authstore.Token) manager.Caller {
		clients++
		return &manager.Client{
			Endpoint: current.Endpoint, Token: current.Value,
			HTTPClient: &http.Client{Transport: sessionTransportFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, "new.example", r.URL.Host)
				return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			})},
		}
	})
	require.NoError(t, err)
	token.Endpoint = "https://new.example/mcp"
	_, err = s.Call(context.Background(), manager.ToolUpdateRepository, nil)
	require.ErrorIs(t, err, authstore.ErrAuthRequired)
	require.Contains(t, err.Error(), token.Endpoint)
	require.Equal(t, 2, clients)
	require.Equal(t, 1, requests)
}

// Rotation must not mutate a client still being used by another call.
func TestSessionRefreshKeepsInFlightCaller(t *testing.T) {
	token := authstore.Token{Value: "synthetic-old"}
	started, resume := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	s, err := openMuster(context.Background(), func(context.Context) (authstore.Token, error) {
		return token, nil
	}, func(current authstore.Token) manager.Caller {
		return sessionCallerFunc(func(context.Context, string, map[string]any) (json.RawMessage, error) {
			if current.Value == "synthetic-old" {
				close(started)
				<-resume
			}
			return json.Marshal(current.Value)
		})
	})
	require.NoError(t, err)
	go func() {
		payload, err := s.Call(context.Background(), manager.ToolGetInfo, nil)
		if err == nil && string(payload) != `"synthetic-old"` {
			err = errors.New("in-flight bearer changed")
		}
		finished <- err
	}()
	<-started
	token.Value = "synthetic-fresh"
	payload, callErr := s.Call(context.Background(), manager.ToolGetInfo, nil)
	close(resume)
	require.NoError(t, <-finished)
	require.NoError(t, callErr)
	require.Equal(t, `"synthetic-fresh"`, string(payload))
}
