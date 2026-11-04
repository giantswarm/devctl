package githubclient

import (
	"context"
	"io"
	"net/http"
	"sync"
)

// reauthorizing sends every request with the current token. When GitHub
// answers 401 it asks renew for another token once and sends the request
// again with it. A 403/404 can also renew when permission confirms that the
// App holds the accepted permissions. The retry's response is the answer.
// Requests refused for the same token while another renewed it are sent
// again with the renewed token, so concurrent refusals renew only once.
type reauthorizing struct {
	base       http.RoundTripper
	renew      func(ctx context.Context, rejected string) (string, error)
	permission func(context.Context, string) string
	refreshed  func(string)

	mu    sync.Mutex
	token string
}

// RoundTrip implements http.RoundTripper.
func (t *reauthorizing) RoundTrip(req *http.Request) (*http.Response, error) {
	sent := t.current()
	resp, err := t.base.RoundTrip(authorized(req, sent))
	if err != nil {
		return resp, err
	}
	// A body that cannot be read again cannot be sent again.
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return resp, nil
	}
	permission := ""
	if resp.StatusCode != http.StatusUnauthorized {
		if (resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound) ||
			resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" || t.permission == nil {
			return resp, nil
		}
		accepted := resp.Header.Get("X-Accepted-GitHub-Permissions")
		if accepted == "" {
			return resp, nil
		}
		permission = t.permission(req.Context(), accepted)
		if permission == "" {
			return resp, nil
		}
	}
	token, err := t.renewed(req.Context(), sent)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	if permission != "" {
		t.refreshed(permission)
	}
	retry := authorized(req, token)
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		retry.Body = body
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return t.base.RoundTrip(retry)
}

func (t *reauthorizing) current() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.token
}

// renewed is the token to send instead of rejected: the one another request
// already renewed it to, or a new one from renew.
func (t *reauthorizing) renewed(ctx context.Context, rejected string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != rejected {
		return t.token, nil
	}
	token, err := t.renew(ctx, rejected)
	if err != nil {
		return "", err
	}
	t.token = token
	return token, nil
}

// authorized is a copy of req carrying token.
func authorized(req *http.Request, token string) *http.Request {
	out := req.Clone(req.Context())
	out.Header.Set("Authorization", "Bearer "+token)
	return out
}
