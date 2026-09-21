package authstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The OAuth form parameters both flows send.
const (
	paramClientID  = "client_id"
	paramGrantType = "grant_type"
	paramCode      = "code"
)

// maxBody bounds what devctl reads of a response; the documents here are a
// few hundred bytes.
const maxBody = 1 << 20

// postForm posts a form and decodes the JSON body into out whatever the
// status: the OAuth endpoints report their errors as JSON. A body that is
// not JSON is an error naming the status, never the body.
func (a *Auth) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return a.do(req, out, http.StatusOK)
}

// postJSON posts a JSON document and decodes the JSON response into out.
func (a *Auth) postJSON(ctx context.Context, endpoint string, in, out any, accepted ...int) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return a.do(req, out, accepted...)
}

// getJSON reads a JSON document with the given headers.
func (a *Auth) getJSON(ctx context.Context, endpoint string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return a.do(req, out, http.StatusOK)
}

// do sends req and decodes a JSON body into out. A status outside accepted
// with a JSON body still decodes (OAuth errors), so the caller reads the
// error fields; without a JSON body it is an error naming the status only.
func (a *Auth) do(req *http.Request, out any, accepted ...int) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "devctl")
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("%s %s: reading the response: %w", req.Method, req.URL.Redacted(), err)
	}
	ok := len(accepted) == 0
	for _, s := range accepted {
		ok = ok || resp.StatusCode == s
	}
	if len(body) > 0 && json.Unmarshal(body, out) == nil {
		if ok || isOAuthError(out) {
			return nil
		}
	}
	return fmt.Errorf("%s %s: HTTP %d", req.Method, req.URL.Redacted(), resp.StatusCode)
}

// oauthErrorer is a response document that can carry an OAuth error.
type oauthErrorer interface {
	oauthError() string
}

func isOAuthError(out any) bool {
	e, ok := out.(oauthErrorer)
	return ok && e.oauthError() != ""
}

// oauthToken is the token endpoint's answer, success or error, of both
// GitHub and CircleCI.
type oauthToken struct {
	AccessToken           string `json:"access_token"`
	TokenType             string `json:"token_type"`
	Scope                 string `json:"scope"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

func (t *oauthToken) oauthError() string { return t.Error }
