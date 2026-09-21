package authstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// CircleCI's OAuth 2.0 endpoints under the issuer, as its authorization
// server metadata publishes them, and the flow's constants.
const (
	circleCIRegisterPath  = "/oauth/register"
	circleCIAuthorizePath = "/oauth/authorize"
	circleCIExchangePath  = "/oauth/token"
	circleCIMePath        = "/me"

	circleCIClientName         = "devctl"
	grantTypeAuthorizationCode = "authorization_code"
	circleCICallbackPath       = "/callback"

	// circleCIAuthorizeTimeout is how long the human has to grant access.
	circleCIAuthorizeTimeout = 10 * time.Minute
)

// clientRegistration is the dynamic client registration request: a public
// client without a secret, redirecting to one loopback address.
type clientRegistration struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type clientRegistered struct {
	ClientID         string `json:"client_id"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (c *clientRegistered) oauthError() string { return c.Error }

type circleCIUser struct {
	Login string `json:"login"`
}

// LoginCircleCI runs the authorization code flow with PKCE: listens on the
// loopback address of the device's client (registering a client first when
// the device has none, or when its address cannot be bound any more), prints
// the authorization URL to stderr, opens the browser, exchanges the code,
// reads the login and stores the record. Re-authorizing the same client
// revokes the previous token on CircleCI's side.
func (a *Auth) LoginCircleCI(ctx context.Context) (Identity, error) {
	previous, err := a.store.Get(UserCircleCI)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Identity{}, err
	}

	listener, redirectURI, err := listenLoopback(previous.RedirectURI)
	if err != nil {
		return Identity{}, err
	}
	defer func() { _ = listener.Close() }()

	clientID := previous.ClientID
	if clientID == "" || redirectURI != previous.RedirectURI {
		clientID, err = a.registerCircleCIClient(ctx, redirectURI)
		if err != nil {
			return Identity{}, err
		}
	}

	verifier, challenge, err := pkce()
	if err != nil {
		return Identity{}, err
	}
	state, err := randomState()
	if err != nil {
		return Identity{}, err
	}
	authorize := a.endpoints.CircleCIOAuthURL + circleCIAuthorizePath + "?" + url.Values{
		"response_type":         {paramCode},
		paramClientID:           {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}.Encode()

	codes, stop := serveCallback(listener, state)
	defer stop()

	fmt.Fprintf(a.stderr, "CircleCI: open %s and grant Read access\n", authorize)
	a.browse(authorize)

	waitCtx, cancel := a.clock.Timeout(ctx, circleCIAuthorizeTimeout)
	defer cancel()
	var code string
	select {
	case result := <-codes:
		if result.err != nil {
			return Identity{}, fmt.Errorf("CircleCI authorization: %w", result.err)
		}
		code = result.code
	case <-waitCtx.Done():
		return Identity{}, fmt.Errorf("CircleCI authorization not completed in time: %w", waitCtx.Err())
	}

	form := url.Values{
		paramGrantType:  {grantTypeAuthorizationCode},
		paramCode:       {code},
		paramClientID:   {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	var token oauthToken
	if err := a.postForm(ctx, a.endpoints.CircleCIOAuthURL+circleCIExchangePath, form, &token); err != nil {
		return Identity{}, fmt.Errorf("exchanging the CircleCI code: %w", err)
	}
	if token.Error != "" {
		return Identity{}, fmt.Errorf("exchanging the CircleCI code: %s", token.Error)
	}
	if token.AccessToken == "" {
		return Identity{}, errors.New("exchanging the CircleCI code: empty token")
	}

	login, err := a.circleCILogin(ctx, token.AccessToken)
	if err != nil {
		return Identity{}, err
	}

	now := a.clock.Now()
	record := Record{Login: login, Token: token.AccessToken, ClientID: clientID, RedirectURI: redirectURI}
	if token.ExpiresIn > 0 {
		record.ExpiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second).UTC()
	}
	if err := a.store.Set(UserCircleCI, record); err != nil {
		return Identity{}, err
	}
	return describe(UserCircleCI, record, now), nil
}

// registerCircleCIClient registers devctl on this device as a public client
// for redirectURI and returns the client id.
func (a *Auth) registerCircleCIClient(ctx context.Context, redirectURI string) (string, error) {
	registration := clientRegistration{
		ClientName:              circleCIClientName,
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{grantTypeAuthorizationCode},
		ResponseTypes:           []string{paramCode},
		TokenEndpointAuthMethod: "none",
	}
	var registered clientRegistered
	err := a.postJSON(ctx, a.endpoints.CircleCIOAuthURL+circleCIRegisterPath, registration, &registered, http.StatusCreated, http.StatusOK)
	if err != nil {
		return "", fmt.Errorf("registering devctl as a CircleCI OAuth client: %w", err)
	}
	if registered.Error != "" {
		return "", fmt.Errorf("registering devctl as a CircleCI OAuth client: %s", registered.Error)
	}
	if registered.ClientID == "" {
		return "", errors.New("registering devctl as a CircleCI OAuth client: no client_id in the response")
	}
	return registered.ClientID, nil
}

func (a *Auth) circleCILogin(ctx context.Context, token string) (string, error) {
	var user circleCIUser
	headers := map[string]string{"Circle-Token": token}
	if err := a.getJSON(ctx, a.endpoints.CircleCIAPIURL+circleCIMePath, headers, &user); err != nil {
		return "", fmt.Errorf("reading the CircleCI login: %w", err)
	}
	if user.Login == "" {
		return "", errors.New("reading the CircleCI login: empty login")
	}
	return user.Login, nil
}

// RequireCircleCI is the CircleCI token while it is valid and
// [ErrAuthRequired] when missing or expired; there is no refresh. The
// token's Warning is set within [CircleCIExpiryWarning] of the expiry.
func (a *Auth) RequireCircleCI(_ context.Context) (Token, error) {
	record, err := a.store.Get(UserCircleCI)
	if errors.Is(err, ErrNotFound) {
		return Token{}, &AuthRequiredError{Identity: identityCircleCI, Cause: causeNoToken, Hint: hintLogin}
	}
	if err != nil {
		return Token{}, err
	}
	now := a.clock.Now()
	if expired(record.ExpiresAt, now) {
		return Token{}, &AuthRequiredError{Identity: identityCircleCI, Cause: "the token expired", Hint: hintLoginCircleCI}
	}
	return Token{
		Value:     record.Token,
		Login:     record.Login,
		ExpiresAt: record.ExpiresAt,
		Warning:   circleCIWarning(record.ExpiresAt, now),
	}, nil
}

// listenLoopback binds the redirect address of the device's client, or a
// fresh loopback port when there is none or it is taken, and returns the
// listener with the redirect URI it serves.
func listenLoopback(previous string) (net.Listener, string, error) {
	if previous != "" {
		if u, err := url.Parse(previous); err == nil && u.Host != "" {
			if l, err := net.Listen("tcp", u.Host); err == nil {
				return l, previous, nil
			}
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("listening for the CircleCI callback: %w", err)
	}
	return l, "http://" + l.Addr().String() + circleCICallbackPath, nil
}

type callbackResult struct {
	code string
	err  error
}

// serveCallback answers the browser's redirect once: the code when the
// state matches, the error CircleCI sent otherwise.
func serveCallback(l net.Listener, state string) (<-chan callbackResult, func()) {
	results := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(circleCICallbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var result callbackResult
		switch {
		case q.Get("state") != state:
			result.err = errors.New("the callback's state does not match the request")
		case q.Get("error") != "":
			result.err = fmt.Errorf("refused in the browser: %s %s", q.Get("error"), q.Get("error_description"))
		case q.Get(paramCode) == "":
			result.err = errors.New("the callback carries no code")
		default:
			result.code = q.Get(paramCode)
		}
		if result.err != nil {
			http.Error(w, "devctl: "+result.err.Error(), http.StatusBadRequest)
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<!doctype html><title>devctl</title><p>devctl is authorized on CircleCI. You can close this tab.</p>")
		}
		select {
		case results <- result:
		default:
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(l) }()
	return results, func() { _ = server.Close() }
}

// pkce is a fresh code verifier and its S256 challenge.
func pkce() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
