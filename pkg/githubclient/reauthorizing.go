package githubclient

import (
	"context"
	"io"
	"net/http"
	"sync"
)

// reauthorizing sends every request with the current token. When GitHub
// answers 401 it asks renew for another token once and sends the request
// again with it; a 401 to that one is the answer. Requests that got 401 for
// the same token while another renewed it are sent again with the renewed
// token, so a run renews once however many of its requests were refused.
type reauthorizing struct {
	base  http.RoundTripper
	renew func(ctx context.Context, rejected string) (string, error)

	mu    sync.Mutex
	token string
}

// RoundTrip implements http.RoundTripper.
func (t *reauthorizing) RoundTrip(req *http.Request) (*http.Response, error) {
	sent := t.current()
	resp, err := t.base.RoundTrip(authorized(req, sent))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	// A body that cannot be read again cannot be sent again.
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return resp, nil
	}
	token, err := t.renewed(req.Context(), sent)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
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
