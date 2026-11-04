package circleciclient

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/giantswarm/microerror"
)

// reauthorizing retries a refused read once, after allowing a transient
// refusal to pass and reading the caller's credential source again. Writes
// are never repeated. The replacement token is retained for later reads.
type reauthorizing struct {
	base  http.RoundTripper
	renew func(context.Context, string) (string, error)

	mu    sync.Mutex
	token string
}

func (t *reauthorizing) RoundTrip(req *http.Request) (*http.Response, error) {
	// Signed step-output URLs share the HTTP client but must receive no
	// CircleCI credential, including after another request renewed it.
	if req.Header.Get("Circle-Token") == "" {
		return t.base.RoundTrip(req)
	}
	t.mu.Lock()
	sent := t.token
	t.mu.Unlock()
	resp, err := t.base.RoundTrip(circleAuthorized(req, sent))
	if err != nil || resp.StatusCode != http.StatusUnauthorized ||
		(req.Method != http.MethodGet && req.Method != http.MethodHead) {
		return resp, err
	}
	// These reads have no body; do not replay a caller's unusual GET body.
	if req.Body != nil && req.Body != http.NoBody {
		return resp, nil
	}
	_ = resp.Body.Close()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-timer.C:
	}
	token, err := t.renewed(req.Context(), sent)
	if err != nil {
		return nil, err
	}
	return t.base.RoundTrip(circleAuthorized(req, token))
}

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
	if token == "" {
		return "", microerror.Maskf(invalidConfigError, "renewing the CircleCI token: empty token")
	}
	t.token = token
	return token, nil
}

func circleAuthorized(req *http.Request, token string) *http.Request {
	out := req.Clone(req.Context())
	out.Header.Set("Circle-Token", token)
	return out
}
