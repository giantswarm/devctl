package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

// Clusters open an installation's management cluster as the person a call
// is made for.
type Clusters interface {
	// Open is the client of installation's management cluster acting as the
	// caller in ctx, its requests going through wrap: auth_required without
	// the person's token, not_applicable for an installation the server does
	// not reach.
	Open(ctx context.Context, installation string, wrap func(http.RoundTripper) http.RoundTripper) (dynamic.Interface, error)
	// Installations are the names of the installations it reaches.
	Installations() []string
}

// NoClusters is the server that reaches no installation: every rollout wait
// is not_applicable.
type NoClusters struct{}

// Installations implements [Clusters].
func (NoClusters) Installations() []string { return nil }

// Open implements [Clusters].
func (NoClusters) Open(_ context.Context, installation string, _ func(http.RoundTripper) http.RoundTripper) (dynamic.Interface, error) {
	return nil, notReached(installation, nil)
}

func notReached(installation string, reached []string) error {
	return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
		"this devctl server reaches no installation %q (it reaches %s): devctl rollout wait on a laptop reads any installation through its Teleport context", installation, orNone(reached))
}

func orNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// Installation is one management cluster the server reaches as the person:
// its API, and its Dex, where the person's token is exchanged for one of
// that installation (RFC 8693) with the client the hub's muster presents
// there. The server holds no credential of the cluster itself.
type Installation struct {
	// Name is the installation's name, the tool's argument.
	Name string `json:"name"`
	// APIServer is the URL of the management cluster's Kubernetes API.
	APIServer string `json:"apiServer"`
	// CAFile is the API's certificate authority, PEM; empty trusts the
	// system's roots.
	CAFile string `json:"caFile,omitempty"`
	// Dex is the installation's Dex.
	Dex Dex `json:"dex"`
}

// Dex is the token exchange at an installation's Dex.
type Dex struct {
	// TokenEndpoint is Dex's token endpoint, https.
	TokenEndpoint string `json:"tokenEndpoint"`
	// Issuer is the issuer the exchanged token must name; empty is the
	// token endpoint without /token.
	Issuer string `json:"issuer,omitempty"`
	// ConnectorID is the connector of Dex that trusts the hub's Dex, which
	// issued the person's token.
	ConnectorID string `json:"connectorId"`
	// Audience is the client the API server accepts tokens of, asked for
	// as a cross-client audience; empty is dex-k8s-authenticator.
	Audience string `json:"audience,omitempty"`
	// ClientIDFile and ClientSecretFile hold the client the exchange
	// authenticates as, read when the server starts.
	ClientIDFile     string `json:"clientIdFile"`
	ClientSecretFile string `json:"clientSecretFile"`
}

// DefaultAudience is the client the API server of a Giant Swarm management
// cluster accepts OIDC tokens of.
const DefaultAudience = "dex-k8s-authenticator"

// The token types of an RFC 8693 exchange at Dex.
const (
	grantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange" //nolint:gosec // G101: a grant type, not a credential
	tokenTypeIDToken   = "urn:ietf:params:oauth:token-type:id_token"       //nolint:gosec // G101: a token type, not a credential
)

// exchangeMargin is how long before its expiry an exchanged token is
// exchanged again.
const exchangeMargin = 30 * time.Second

var installationName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ReadInstallations reads the installations file: a JSON object with an
// "installations" list. The client files are read here, once.
func ReadInstallations(path string) (*DexExchange, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the operator's --installations file
	if err != nil {
		return nil, fmt.Errorf("reading the installations: %w", err)
	}
	var file struct {
		Installations []Installation `json:"installations"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("reading the installations %s: %w", path, err)
	}
	return NewDexExchange(file.Installations)
}

// NewDexExchange validates installations and reads their client files.
func NewDexExchange(installations []Installation) (*DexExchange, error) {
	d := &DexExchange{installations: map[string]installation{}}
	for _, i := range installations {
		if !installationName.MatchString(i.Name) {
			return nil, fmt.Errorf("installation %q: not an installation name", i.Name)
		}
		if _, ok := d.installations[i.Name]; ok {
			return nil, fmt.Errorf("installation %s: named twice", i.Name)
		}
		for what, u := range map[string]string{"apiServer": i.APIServer, "dex.tokenEndpoint": i.Dex.TokenEndpoint} {
			if parsed, err := url.Parse(u); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
				return nil, fmt.Errorf("installation %s: %s %q is not an https URL", i.Name, what, u)
			}
		}
		if i.Dex.ConnectorID == "" {
			return nil, fmt.Errorf("installation %s: dex.connectorId is empty", i.Name)
		}
		clientID, err := readFile(i.Dex.ClientIDFile)
		if err != nil {
			return nil, fmt.Errorf("installation %s: the client id: %w", i.Name, err)
		}
		clientSecret, err := readFile(i.Dex.ClientSecretFile)
		if err != nil {
			return nil, fmt.Errorf("installation %s: the client secret: %w", i.Name, err)
		}
		if i.Dex.Issuer == "" {
			i.Dex.Issuer = strings.TrimSuffix(i.Dex.TokenEndpoint, "/token")
		}
		if i.Dex.Audience == "" {
			i.Dex.Audience = DefaultAudience
		}
		d.installations[i.Name] = installation{Installation: i, clientID: clientID, clientSecret: clientSecret}
		d.names = append(d.names, i.Name)
	}
	return d, nil
}

func readFile(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("no file named")
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: a client file the installations file names
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return value, nil
}

// DexExchange opens the installations it was given as the person: it
// exchanges the person's token, which the gateway in front forwards in
// Authorization (muster's forwardToken), at the installation's Dex for a
// token its API accepts, and sends that with every request. The cluster
// answers by the person's access; the server holds no kubeconfig and no
// credential of a cluster.
type DexExchange struct {
	// Client sends the exchanges; nil is http.DefaultClient.
	Client *http.Client
	// Now is the clock of the exchanged tokens' expiry; nil is time.Now.
	Now func() time.Time

	installations map[string]installation
	names         []string
	mu            sync.Mutex
	exchanged     map[[sha256.Size]byte]exchangedToken
}

type installation struct {
	Installation
	clientID, clientSecret string
}

type exchangedToken struct {
	token   string
	expires time.Time
}

// Installations implements [Clusters].
func (d *DexExchange) Installations() []string { return d.names }

// Open implements [Clusters].
func (d *DexExchange) Open(ctx context.Context, name string, wrap func(http.RoundTripper) http.RoundTripper) (dynamic.Interface, error) {
	i, ok := d.installations[name]
	if !ok {
		return nil, notReached(name, d.names)
	}
	subject := callFrom(ctx).bearer
	if subject == "" {
		return nil, agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired,
			"the devctl server reads %s as you with your Dex token, which muster forwards: none came with this call; sign in to muster again, then call again", name)
	}
	// The first exchange is made before the wait, so a refusal ends the
	// call at once.
	if _, err := d.token(ctx, i, subject); err != nil {
		return nil, err
	}
	config := &rest.Config{Host: i.APIServer, TLSClientConfig: rest.TLSClientConfig{CAFile: i.CAFile}}
	return rolloutwait.NewClient(config, func(base http.RoundTripper) http.RoundTripper {
		var rt http.RoundTripper = &bearer{base: base, token: func(ctx context.Context) (string, error) { return d.token(ctx, i, subject) }}
		if wrap != nil {
			rt = wrap(rt)
		}
		return rt
	})
}

// bearer sends every request with the exchanged token.
type bearer struct {
	base  http.RoundTripper
	token func(ctx context.Context) (string, error)
}

func (b *bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	token, err := b.token(r.Context())
	if err != nil {
		return nil, err
	}
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	return b.base.RoundTrip(r)
}

// token is the person's token of installation i, exchanged for subject,
// kept until shortly before it expires.
func (d *DexExchange) token(ctx context.Context, i installation, subject string) (string, error) {
	now := d.now
	key := sha256.Sum256([]byte(i.Name + "\x00" + subject))
	d.mu.Lock()
	kept, ok := d.exchanged[key]
	d.mu.Unlock()
	if ok && now().Before(kept.expires) {
		return kept.token, nil
	}
	token, expires, err := d.exchange(ctx, i, subject)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.exchanged == nil {
		d.exchanged = map[[sha256.Size]byte]exchangedToken{}
	}
	for k, v := range d.exchanged {
		if !now().Before(v.expires) {
			delete(d.exchanged, k)
		}
	}
	d.exchanged[key] = exchangedToken{token: token, expires: expires.Add(-exchangeMargin)}
	return token, nil
}

// exchange asks i's Dex for an ID token of the person subject names, with
// the API server's client as its audience.
func (d *DexExchange) exchange(ctx context.Context, i installation, subject string) (string, time.Time, error) {
	form := url.Values{
		"grant_type":           {grantTokenExchange},
		"subject_token":        {subject},
		"subject_token_type":   {tokenTypeIDToken},
		"requested_token_type": {tokenTypeIDToken},
		"connector_id":         {i.Dex.ConnectorID},
		"scope":                {"openid profile email groups audience:server:client_id:" + i.Dex.Audience},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.Dex.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(url.QueryEscape(i.clientID), url.QueryEscape(i.clientSecret))
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("exchanging your token at the Dex of %s: %w", i.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		AccessToken      string `json:"access_token"`
		IssuedTokenType  string `json:"issued_token_type"`
		ExpiresIn        int64  `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil && resp.StatusCode == http.StatusOK {
		return "", time.Time{}, fmt.Errorf("exchanging your token at the Dex of %s: %w", i.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		refusal := strings.TrimSpace(body.Error + ": " + body.ErrorDescription)
		if body.Error == "" {
			refusal = resp.Status
		}
		// Dex refuses the person's token (expired, or not of the Dex its
		// connector trusts) as invalid_request or invalid_grant; a 5xx or
		// a refused client is the server's.
		if resp.StatusCode == http.StatusBadRequest && body.Error != "invalid_client" {
			return "", time.Time{}, agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired,
				"the Dex of %s refused your token (%s): sign in to muster again, then call again", i.Name, refusal)
		}
		return "", time.Time{}, fmt.Errorf("exchanging your token at the Dex of %s: %s", i.Name, refusal)
	}
	if body.IssuedTokenType != "" && body.IssuedTokenType != tokenTypeIDToken {
		return "", time.Time{}, fmt.Errorf("exchanging your token at the Dex of %s: issued %s, not an ID token", i.Name, body.IssuedTokenType)
	}
	claims, err := jwtClaims(body.AccessToken)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("exchanging your token at the Dex of %s: %w", i.Name, err)
	}
	if claims.Issuer != i.Dex.Issuer {
		return "", time.Time{}, fmt.Errorf("exchanging your token at the Dex of %s: the token's issuer is %q, not %q", i.Name, claims.Issuer, i.Dex.Issuer)
	}
	expires := time.Unix(claims.Expiry, 0)
	if body.ExpiresIn > 0 {
		expires = d.now().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	return body.AccessToken, expires, nil
}

func (d *DexExchange) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// tokenClaims are what the server reads of an exchanged token.
type tokenClaims struct {
	Issuer string `json:"iss"`
	Expiry int64  `json:"exp"`
}

// jwtClaims are the issuer and expiry of a JWT, unverified: the API server
// verifies the token, the issuer check only keeps a token of another Dex
// from being sent there.
func jwtClaims(token string) (tokenClaims, error) {
	var claims tokenClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, fmt.Errorf("the issued token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, fmt.Errorf("the issued token is not a JWT: %w", err)
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, fmt.Errorf("the issued token is not a JWT: %w", err)
	}
	return claims, nil
}
