package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// Caller is the person a call is made for, as GitHub names the holder of
// their grant's token.
type Caller struct {
	// Login is the person's GitHub login.
	Login string
	// ID is the person's GitHub user id, stable across a renamed login.
	ID int64
}

// Users answer the GitHub user a token of the App acts as.
type Users interface {
	User(ctx context.Context, token string) (Caller, error)
}

// GitHubUsers read GET /user at BaseURL and keep each token's answer for
// TTL: a token GitHub refuses is auth_required.
type GitHubUsers struct {
	BaseURL string
	Client  *http.Client
	TTL     time.Duration
	Now     func() time.Time

	cache tokenCache[Caller]
}

// DefaultUsersTTL is how long a token's user is kept.
const DefaultUsersTTL = 5 * time.Minute

// User implements [Users].
func (g *GitHubUsers) User(ctx context.Context, token string) (Caller, error) {
	return g.cache.get(token, g.TTL, DefaultUsersTTL, g.Now, func() (Caller, error) { return g.read(ctx, token) })
}

func (g *GitHubUsers) read(ctx context.Context, token string) (Caller, error) {
	var user struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	}
	if err := getGitHub(ctx, g.Client, strings.TrimSuffix(g.BaseURL, "/")+"/user", token, "your GitHub user", &user); err != nil {
		return Caller{}, err
	}
	if user.Login == "" || user.ID == 0 {
		return Caller{}, fmt.Errorf("reading your GitHub user: GitHub named no login")
	}
	return Caller{Login: user.Login, ID: user.ID}, nil
}

// getGitHub reads url with token into body: auth_required when GitHub
// refuses the token, which the person grants again in muster.
func getGitHub(ctx context.Context, client *http.Client, url, token, what string, body any) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reading %s: %w", what, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired,
			"GitHub refused your token of the App: grant it again in muster, then call again")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("reading %s: GitHub answered %s", what, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		return fmt.Errorf("reading %s: %w", what, err)
	}
	return nil
}

// tokenCache keeps one answer per token, by the token's hash, until it
// expires; an error is never kept.
type tokenCache[T any] struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]cached[T]
}

type cached[T any] struct {
	value   T
	expires time.Time
}

// get is token's kept answer, or read's, kept for ttl (fallback when ttl is
// not positive).
func (c *tokenCache[T]) get(token string, ttl, fallback time.Duration, now func() time.Time, read func() (T, error)) (T, error) {
	if now == nil {
		now = time.Now
	}
	key := sha256.Sum256([]byte(token))
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if ok && now().Before(entry.expires) {
		return entry.value, nil
	}
	value, err := read()
	if err != nil {
		return value, err
	}
	if ttl <= 0 {
		ttl = fallback
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[[sha256.Size]byte]cached[T]{}
	}
	for k, v := range c.entries {
		if !now().Before(v.expires) {
			delete(c.entries, k)
		}
	}
	c.entries[key] = cached[T]{value: value, expires: now().Add(ttl)}
	return value, nil
}
