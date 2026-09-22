package authstore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// muster is an MCP endpoint behind its own OAuth 2.1 authorization server:
// the endpoint's protected-resource metadata (RFC 9728) names the server and
// the server's metadata (RFC 8414) names its endpoints. devctl registers no
// client there -- muster gates its registration endpoint with a token a CLI
// on every laptop cannot carry -- and identifies itself with a client ID
// metadata document (CIMD), the way the muster agent does: the client_id is
// the URL of a hosted JSON document naming the client, its loopback redirect
// URIs and its grants, which the server fetches once. The token is bound to
// the endpoint with the resource indicator (RFC 8707) and refreshed with the
// refresh token the offline_access scope grants.
const (
	protectedResourcePath   = "/.well-known/oauth-protected-resource"
	authorizationServerPath = "/.well-known/oauth-authorization-server"
	openIDConfigurationPath = "/.well-known/openid-configuration"

	// musterAuthorizeTimeout is how long the human has to sign in.
	musterAuthorizeTimeout = 10 * time.Minute
)

// MusterClientID is devctl's client ID metadata document: served by the
// muster repository's GitHub Pages next to the muster agent's, it names the
// public client devctl with loopback redirect URIs on any port (RFC 8252),
// the authorization code and refresh token grants and the agent's scopes.
// A client id that is a URL discloses nothing; the document is public.
const MusterClientID = "https://giantswarm.github.io/muster/devctl.json"

// musterScopes are requested when the endpoint's metadata names none: the
// OIDC claims muster's own agent asks for, and offline_access for the refresh
// token.
var musterScopes = []string{"openid", "profile", "email", "groups", "offline_access"}

// protectedResource is the endpoint's RFC 9728 document.
type protectedResource struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

// authorizationServer is the server's RFC 8414 (or OpenID) document.
type authorizationServer struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	UserinfoEndpoint                  string   `json:"userinfo_endpoint"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
}

// musterUser is the userinfo answer; the login is the first of these that is set.
type musterUser struct {
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	Sub               string `json:"sub"`
}

func (u musterUser) login() string {
	for _, v := range []string{u.Email, u.PreferredUsername, u.Name, u.Sub} {
		if v != "" {
			return v
		}
	}
	return ""
}

// musterDiscovery is what the two documents say about an endpoint.
type musterDiscovery struct {
	// Resource is the identifier the token is bound to: the endpoint's
	// declared one, else the endpoint itself.
	Resource string
	Scopes   []string
	Server   authorizationServer
}

// discoverMuster reads the endpoint's protected-resource metadata, the
// path-inserted well-known URL first, then its authorization server's
// metadata, and refuses a server without S256 PKCE or without client ID
// metadata documents.
func (a *Auth) discoverMuster(ctx context.Context, endpoint string) (musterDiscovery, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return musterDiscovery{}, fmt.Errorf("the muster endpoint %q is not a URL", endpoint)
	}
	origin := u.Scheme + "://" + u.Host
	candidates := []string{origin + protectedResourcePath}
	if p := strings.TrimRight(u.Path, "/"); p != "" {
		candidates = []string{origin + protectedResourcePath + p, origin + protectedResourcePath}
	}
	var resource protectedResource
	var found bool
	var lastErr error
	for _, candidate := range candidates {
		var doc protectedResource
		if err := a.getJSON(ctx, candidate, nil, &doc); err != nil {
			lastErr = err
			continue
		}
		if len(doc.AuthorizationServers) > 0 {
			resource, found = doc, true
			break
		}
		lastErr = fmt.Errorf("%s names no authorization server", candidate)
	}
	if !found {
		return musterDiscovery{}, fmt.Errorf("discovering the authorization server of %s: %w", endpoint, lastErr)
	}
	issuer := strings.TrimRight(resource.AuthorizationServers[0], "/")

	var server authorizationServer
	if err := a.getJSON(ctx, issuer+authorizationServerPath, nil, &server); err != nil {
		if err2 := a.getJSON(ctx, issuer+openIDConfigurationPath, nil, &server); err2 != nil {
			return musterDiscovery{}, fmt.Errorf("reading the metadata of the authorization server %s: %w", issuer, err)
		}
	}
	if server.AuthorizationEndpoint == "" || server.TokenEndpoint == "" {
		return musterDiscovery{}, fmt.Errorf("the authorization server %s names no authorization or token endpoint", issuer)
	}
	if !slices.Contains(server.CodeChallengeMethodsSupported, "S256") {
		return musterDiscovery{}, fmt.Errorf("the authorization server %s does not support S256 PKCE", issuer)
	}
	if !server.ClientIDMetadataDocumentSupported {
		return musterDiscovery{}, fmt.Errorf("the authorization server %s does not support client ID metadata documents, which is how devctl identifies itself (%s)", issuer, MusterClientID)
	}
	if server.Issuer == "" {
		server.Issuer = issuer
	}

	d := musterDiscovery{Resource: resource.Resource, Scopes: resource.ScopesSupported, Server: server}
	if d.Resource == "" {
		d.Resource = endpoint
	}
	if len(d.Scopes) == 0 {
		d.Scopes = musterScopes
	}
	return d, nil
}

// LoginMuster runs the authorization code flow with PKCE against the
// authorization server the endpoint names: discovers it, listens on a
// loopback address (the device's last one when it can be bound, else a
// fresh port -- muster matches loopback redirects on any port), prints the
// authorization URL to stderr with [MusterClientID] as the client, opens
// the browser, exchanges the code, reads the login from userinfo and stores
// the record with the endpoint and the issuer.
func (a *Auth) LoginMuster(ctx context.Context, endpoint string) (Identity, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return Identity{}, errors.New("the muster endpoint must not be empty")
	}
	discovered, err := a.discoverMuster(ctx, endpoint)
	if err != nil {
		return Identity{}, err
	}
	server := discovered.Server

	previous, err := a.store.Get(UserMuster)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Identity{}, err
	}

	listener, redirectURI, err := listenLoopback(previous.RedirectURI)
	if err != nil {
		return Identity{}, err
	}
	defer func() { _ = listener.Close() }()

	clientID := MusterClientID

	verifier, challenge, err := pkce()
	if err != nil {
		return Identity{}, err
	}
	state, err := randomState()
	if err != nil {
		return Identity{}, err
	}
	authorize, err := withQuery(server.AuthorizationEndpoint, url.Values{
		"response_type":         {paramCode},
		paramClientID:           {clientID},
		paramRedirectURI:        {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"scope":                 {strings.Join(discovered.Scopes, " ")},
		paramResource:           {discovered.Resource},
	})
	if err != nil {
		return Identity{}, err
	}

	codes, stop := serveCallback(listener, state, "devctl is signed in to muster. You can close this tab.")
	defer stop()

	fmt.Fprintf(a.stderr, "muster: open %s and sign in\n", authorize)
	a.browse(authorize)

	waitCtx, cancel := a.clock.Timeout(ctx, musterAuthorizeTimeout)
	defer cancel()
	var code string
	select {
	case result := <-codes:
		if result.err != nil {
			return Identity{}, fmt.Errorf("muster sign-in: %w", result.err)
		}
		code = result.code
	case <-waitCtx.Done():
		return Identity{}, fmt.Errorf("muster sign-in not completed in time: %w", waitCtx.Err())
	}

	form := url.Values{
		paramGrantType:   {grantTypeAuthorizationCode},
		paramCode:        {code},
		paramClientID:    {clientID},
		paramRedirectURI: {redirectURI},
		"code_verifier":  {verifier},
		paramResource:    {discovered.Resource},
	}
	var token oauthToken
	if err := a.postForm(ctx, server.TokenEndpoint, form, &token); err != nil {
		return Identity{}, fmt.Errorf("exchanging the muster code: %w", err)
	}
	if token.Error != "" {
		return Identity{}, fmt.Errorf("exchanging the muster code: %s", token.Error)
	}
	if token.AccessToken == "" {
		return Identity{}, errors.New("exchanging the muster code: empty token")
	}

	login, err := a.musterLogin(ctx, server.UserinfoEndpoint, token.AccessToken)
	if err != nil {
		return Identity{}, err
	}

	now := a.clock.Now()
	record := musterRecord(&token, now)
	record.Login = login
	record.ClientID = clientID
	record.RedirectURI = redirectURI
	record.Endpoint = endpoint
	record.Issuer = server.Issuer
	if err := a.store.Set(UserMuster, record); err != nil {
		return Identity{}, err
	}
	return describe(UserMuster, record, now), nil
}

// musterLogin reads who the token acts as from the userinfo endpoint; a
// server without one leaves the login empty.
func (a *Auth) musterLogin(ctx context.Context, userinfo, token string) (string, error) {
	if userinfo == "" {
		return "", nil
	}
	var user musterUser
	headers := map[string]string{"Authorization": "Bearer " + token}
	if err := a.getJSON(ctx, userinfo, headers, &user); err != nil {
		return "", fmt.Errorf("reading the muster login: %w", err)
	}
	return user.login(), nil
}

// refreshMuster trades the refresh token for a new pair at the issuer's
// token endpoint and stores it; a refresh token the answer does not rotate
// stays.
func (a *Auth) refreshMuster(ctx context.Context, record Record) (Record, error) {
	discovered, err := a.discoverMuster(ctx, record.Endpoint)
	if err != nil {
		return Record{}, fmt.Errorf("refreshing the muster token: %w", err)
	}
	form := url.Values{
		paramGrantType:  {grantTypeRefresh},
		"refresh_token": {record.RefreshToken},
		paramClientID:   {MusterClientID},
		paramResource:   {discovered.Resource},
	}
	var token oauthToken
	if err := a.postForm(ctx, discovered.Server.TokenEndpoint, form, &token); err != nil {
		return Record{}, fmt.Errorf("refreshing the muster token: %w", err)
	}
	if token.Error != "" || token.AccessToken == "" {
		cause := "the token expired and muster refused the refresh"
		if token.Error != "" {
			cause += " (" + token.Error + ")"
		}
		return Record{}, &AuthRequiredError{Identity: identityMuster, Cause: cause, Hint: hintLoginMuster}
	}
	fresh := musterRecord(&token, a.clock.Now())
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = record.RefreshToken
	}
	fresh.Login = record.Login
	fresh.ClientID = record.ClientID
	fresh.RedirectURI = record.RedirectURI
	fresh.Endpoint = record.Endpoint
	fresh.Issuer = record.Issuer
	if err := a.store.Set(UserMuster, fresh); err != nil {
		return Record{}, err
	}
	return fresh, nil
}

func musterRecord(token *oauthToken, now time.Time) Record {
	record := Record{Token: token.AccessToken, RefreshToken: token.RefreshToken}
	if token.ExpiresIn > 0 {
		record.ExpiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second).UTC()
	}
	if token.RefreshToken != "" && token.RefreshTokenExpiresIn > 0 {
		record.RefreshExpiresAt = now.Add(time.Duration(token.RefreshTokenExpiresIn) * time.Second).UTC()
	}
	return record
}

// RequireMuster is the muster token: the stored one while it is valid, a
// refreshed one when it expired and the refresh token has not, and
// [ErrAuthRequired] otherwise. Token.Endpoint is the MCP endpoint it is for.
func (a *Auth) RequireMuster(ctx context.Context) (Token, error) {
	record, err := a.store.Get(UserMuster)
	if errors.Is(err, ErrNotFound) {
		return Token{}, &AuthRequiredError{Identity: identityMuster, Cause: causeNoToken, Hint: hintLoginMuster}
	}
	if err != nil {
		return Token{}, err
	}
	now := a.clock.Now()
	if expired(record.ExpiresAt, now) {
		if record.RefreshToken == "" || expired(record.RefreshExpiresAt, now) {
			return Token{}, &AuthRequiredError{Identity: identityMuster, Cause: "the token expired and cannot be refreshed", Hint: hintLoginMuster}
		}
		record, err = a.refreshMuster(ctx, record)
		if err != nil {
			return Token{}, err
		}
	}
	return Token{Value: record.Token, Login: record.Login, ExpiresAt: record.ExpiresAt, Endpoint: record.Endpoint}, nil
}

// withQuery adds values to a URL that may carry a query already.
func withQuery(base string, values url.Values) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("the authorization endpoint %q is not a URL: %w", base, err)
	}
	q := u.Query()
	for k, vs := range values {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
