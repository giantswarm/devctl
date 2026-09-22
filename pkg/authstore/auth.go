// Package authstore holds the identities devctl's agent-facing commands act
// with: a GitHub user access token of the devctl GitHub App, obtained with the
// device flow and refreshed without a human; a CircleCI API token obtained
// with the OAuth 2.0 authorization code flow with PKCE after a one-time
// dynamic client registration per device; and a muster access token for the
// installation that runs giantswarm-repo-manager, obtained the same way from
// muster's own authorization server and refreshed without a human. All live
// in the OS keychain with their expiry and nowhere else: no environment
// variable, no file, no output.
//
// A command that needs a token calls [RequireGitHub], [RequireCircleCI] or
// [RequireMuster] before it does anything else and returns the
// [ErrAuthRequired] it gets unchanged: it is exit 8 with one sentence naming
// `devctl auth login`.
package authstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// expirySkew is how close to its expiry a token still counts as valid: not at
// all, so a wait does not start on a token that dies under it.
const expirySkew = 30 * time.Second

// CircleCIExpiryWarning is how long before a CircleCI token expires the
// commands warn about it; the token has no refresh, a human re-authorizes.
const CircleCIExpiryWarning = 7 * 24 * time.Hour

// ErrAuthRequired is what every [*AuthRequiredError] matches with errors.Is.
var ErrAuthRequired = errors.New("authentication required")

// The identities as the errors name them, the causes and the hints.
const (
	identityGitHub   = "GitHub"
	identityCircleCI = "CircleCI"
	identityMuster   = "muster"

	causeNoToken = "no token in the keychain"

	hintLogin         = "devctl auth login"
	hintLoginGitHub   = "devctl auth login --github-only"
	hintLoginCircleCI = "devctl auth login --circleci-only"
	hintLoginMuster   = "devctl auth login --muster-only"
)

// AuthRequiredError is exit 8: no usable token for Identity. Its message is
// the one sentence the envelope's reason carries.
type AuthRequiredError struct {
	// Identity is "GitHub", "CircleCI" or "muster".
	Identity string
	// Cause says what is wrong with the record, without token material.
	Cause string
	// Hint is the command that fixes it.
	Hint string
}

func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("%s authentication required (%s): run `%s`.", e.Identity, e.Cause, e.Hint)
}

// Is makes errors.Is(err, ErrAuthRequired) true.
func (e *AuthRequiredError) Is(target error) bool { return target == ErrAuthRequired }

// ExitCode implements [agentcli.ExitCoder].
func (e *AuthRequiredError) ExitCode() int { return agentcli.ExitAuthRequired }

// ExitVerdict implements [agentcli.ExitCoder].
func (e *AuthRequiredError) ExitVerdict() agentcli.Verdict { return agentcli.VerdictAuthRequired }

// Token is a usable access token with what a command may say about it.
type Token struct {
	// Value is the token. It goes into a request header and nowhere else.
	Value string
	// Login is the account the token acts as.
	Login string
	// ExpiresAt is zero for a token that does not expire.
	ExpiresAt time.Time
	// Warning is the expiry notice of a CircleCI token within
	// [CircleCIExpiryWarning] of its end, for the envelope; empty otherwise.
	Warning string
	// Endpoint is the muster MCP endpoint a muster token is for; empty for
	// the other identities.
	Endpoint string
}

// Config configures an [Auth].
type Config struct {
	// Store is required.
	Store Store
	// Endpoints default to production.
	Endpoints agentcli.Endpoints
	// Clock defaults to the system clock at scale 1.
	Clock agentcli.Clock
	// HTTPClient defaults to one with a timeout.
	HTTPClient *http.Client
	// OpenBrowser opens a URL for the human; nil means the platform's opener.
	OpenBrowser func(url string) error
	// Stderr receives the instructions for the human during login (the
	// verification URL and code), never a token; nil means os.Stderr.
	Stderr io.Writer
}

// Auth runs the flows and answers the token questions against one store.
type Auth struct {
	store       Store
	endpoints   agentcli.Endpoints
	clock       agentcli.Clock
	http        *http.Client
	openBrowser func(url string) error
	stderr      io.Writer
}

// New returns an Auth for config.
func New(config Config) (*Auth, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("%T.Store must not be empty", config)
	}
	if config.Endpoints == (agentcli.Endpoints{}) {
		config.Endpoints = agentcli.DefaultEndpoints()
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if config.OpenBrowser == nil {
		config.OpenBrowser = OpenBrowser
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	return &Auth{
		store:       config.Store,
		endpoints:   config.Endpoints,
		clock:       config.Clock,
		http:        config.HTTPClient,
		openBrowser: config.OpenBrowser,
		stderr:      config.Stderr,
	}, nil
}

// Open is the Auth the environment configures: endpoints and store from
// [agentcli.EndpointsFromEnv], the clock from [agentcli.SystemClock]. stderr
// nil means os.Stderr.
func Open(stderr io.Writer) (*Auth, error) {
	clock, err := agentcli.SystemClock()
	if err != nil {
		return nil, err
	}
	endpoints := agentcli.EndpointsFromEnv()
	return New(Config{
		Store:     OpenStore(endpoints),
		Endpoints: endpoints,
		Clock:     clock,
		Stderr:    stderr,
	})
}

// RequireGitHub is the GitHub token of the environment's store, refreshed
// when expired and refreshable; [ErrAuthRequired] otherwise.
func RequireGitHub(ctx context.Context) (Token, error) {
	a, err := Open(nil)
	if err != nil {
		return Token{}, err
	}
	return a.RequireGitHub(ctx)
}

// RequireCircleCI is the CircleCI token of the environment's store;
// [ErrAuthRequired] when missing or expired. Token.Warning carries the
// seven-day expiry notice.
func RequireCircleCI(ctx context.Context) (Token, error) {
	a, err := Open(nil)
	if err != nil {
		return Token{}, err
	}
	return a.RequireCircleCI(ctx)
}

// RequireMuster is the muster token of the environment's store, refreshed
// when expired and refreshable; [ErrAuthRequired] otherwise. Token.Endpoint
// is the MCP endpoint the token is for.
func RequireMuster(ctx context.Context) (Token, error) {
	a, err := Open(nil)
	if err != nil {
		return Token{}, err
	}
	return a.RequireMuster(ctx)
}

// Identity is what `auth status` says about one identity: never the token.
type Identity struct {
	// Present is false when the keychain has no record.
	Present bool   `json:"present"`
	Login   string `json:"login,omitempty"`
	// ExpiresAt is absent for a token that does not expire.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Expired   bool       `json:"expired"`
	// Refreshable: the record has a refresh token that has not expired, so a
	// command refreshes the access token itself (GitHub and muster).
	Refreshable      bool       `json:"refreshable"`
	RefreshExpiresAt *time.Time `json:"refreshExpiresAt,omitempty"`
	// Endpoint is the muster MCP endpoint the token is for (muster only).
	Endpoint string   `json:"endpoint,omitempty"`
	Warnings []string `json:"warnings"`
}

// Usable: a command can act with this identity without a human.
func (i Identity) Usable() bool {
	return i.Present && (!i.Expired || i.Refreshable)
}

// Status is the three identities. GitHub and CircleCI are what the pr and
// release commands need; muster is what the repo commands need, and the
// other commands never ask for it.
type Status struct {
	GitHub   Identity `json:"github"`
	CircleCI Identity `json:"circleci"`
	Muster   Identity `json:"muster"`
}

// Status reads the records without touching the network.
func (a *Auth) Status() (Status, error) {
	now := a.clock.Now()
	github, err := a.identity(UserGitHub, now)
	if err != nil {
		return Status{}, err
	}
	circleci, err := a.identity(UserCircleCI, now)
	if err != nil {
		return Status{}, err
	}
	muster, err := a.identity(UserMuster, now)
	if err != nil {
		return Status{}, err
	}
	return Status{GitHub: github, CircleCI: circleci, Muster: muster}, nil
}

// NewStatus is the status before anything is known: every identity absent,
// their warnings empty arrays.
func NewStatus() Status {
	return Status{GitHub: Identity{Warnings: []string{}}, CircleCI: Identity{Warnings: []string{}}, Muster: Identity{Warnings: []string{}}}
}

// Check is nil when the GitHub and CircleCI identities are usable, else the
// [*AuthRequiredError] of the first one that is not, hinting at the login
// that fixes it. The muster identity is not checked: only the repo commands
// need it, and they ask [RequireMuster] themselves.
func (s Status) Check() error {
	switch {
	case !s.GitHub.Usable() && !s.CircleCI.Usable():
		return authRequired(identityGitHub, s.GitHub, hintLogin)
	case !s.GitHub.Usable():
		return authRequired(identityGitHub, s.GitHub, hintLoginGitHub)
	case !s.CircleCI.Usable():
		return authRequired(identityCircleCI, s.CircleCI, hintLoginCircleCI)
	}
	return nil
}

func authRequired(name string, id Identity, hint string) *AuthRequiredError {
	cause := causeNoToken
	if id.Present {
		cause = "the token expired"
		if id.RefreshExpiresAt != nil {
			cause = "the token expired and the refresh token with it"
		}
	}
	return &AuthRequiredError{Identity: name, Cause: cause, Hint: hint}
}

func (a *Auth) identity(user string, now time.Time) (Identity, error) {
	record, err := a.store.Get(user)
	if errors.Is(err, ErrNotFound) {
		return Identity{Warnings: []string{}}, nil
	}
	if err != nil {
		return Identity{}, err
	}
	return describe(user, record, now), nil
}

func describe(user string, record Record, now time.Time) Identity {
	id := Identity{Present: true, Login: record.Login, Warnings: []string{}}
	if !record.ExpiresAt.IsZero() {
		t := record.ExpiresAt.UTC()
		id.ExpiresAt = &t
		id.Expired = expired(record.ExpiresAt, now)
	}
	if (user == UserGitHub || user == UserMuster) && record.RefreshToken != "" {
		id.Refreshable = !expired(record.RefreshExpiresAt, now)
		if !record.RefreshExpiresAt.IsZero() {
			t := record.RefreshExpiresAt.UTC()
			id.RefreshExpiresAt = &t
		}
	}
	if user == UserMuster {
		id.Endpoint = record.Endpoint
	}
	if user == UserCircleCI && !id.Expired {
		if w := circleCIWarning(record.ExpiresAt, now); w != "" {
			id.Warnings = append(id.Warnings, w)
		}
	}
	return id
}

// expired: at is set and now (plus the skew) has reached it.
func expired(at, now time.Time) bool {
	return !at.IsZero() && !now.Add(expirySkew).Before(at)
}

func circleCIWarning(expiresAt, now time.Time) string {
	if expiresAt.IsZero() {
		return ""
	}
	remaining := expiresAt.Sub(now)
	if remaining > CircleCIExpiryWarning {
		return ""
	}
	days := int(math.Ceil(remaining.Hours() / 24))
	if days < 0 {
		days = 0
	}
	return fmt.Sprintf("CircleCI token expires in %d day(s), at %s: run `devctl auth login --circleci-only` before then.",
		days, expiresAt.UTC().Format(time.RFC3339))
}

// browse opens url for the human and, when no browser opens, says so on
// stderr; the URL was printed there already.
func (a *Auth) browse(url string) {
	if err := a.openBrowser(url); err != nil {
		fmt.Fprintf(a.stderr, "(no browser opened: %v; open the URL yourself)\n", err)
	}
}
