package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// GrantHeaderPrefix starts the header the gateway in front carries the
// person's token of one provider App in: muster attaches
// X-Muster-Grant-<app> for every App of the server's registration
// (spec.auth.providerApps) the person granted.
const GrantHeaderPrefix = "X-Muster-Grant-" //nolint:gosec // G101: a header name, not a credential

// StatusGrantRequired is the structuredContent status of a result that
// needs the person's grant of the Apps it names in "apps": muster answers it
// auth_required with each App's sign-in link (authUrl).
const StatusGrantRequired = "grant_required"

// MusterGrant is the person's token of the GitHub App muster keeps as App:
// muster attaches it to every call the person granted it for. The token
// names the caller: a call without one GitHub accepts is refused. The
// server stores no token and runs no sign-in.
type MusterGrant struct {
	// App is the provider App's name in muster, the header's suffix.
	App string
	// GitHubApp is the GitHub App's name, as a refusal names it.
	GitHubApp string
	// Users answer the person the token acts as, the call's caller; nil
	// names none and accepts the token unchecked.
	Users Users
	// Installations answer the owners the App is installed on that the
	// person reaches; nil checks none.
	Installations Installations
}

// GitHubToken implements [Credentials]: the person's token of the App,
// with the caller it names put on the call; a grant_required auth_required
// without one or with one GitHub refuses, and not_applicable for a
// repository whose owner the App is not installed on.
func (g MusterGrant) GitHubToken(ctx context.Context) (string, error) {
	call := callFrom(ctx)
	token := call.grants[textproto.CanonicalMIMEHeaderKey(GrantHeaderPrefix+g.App)]
	if token == "" {
		call.grantRequired = g.App
		return "", agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired,
			"the devctl server acts on GitHub as you through the GitHub App %s: grant it once in muster (core_auth_login app=%s), then call again", g.gitHubApp(), g.App)
	}
	if g.Users != nil {
		caller, err := g.Users.User(ctx, token)
		if err != nil {
			if agentcli.Exit(err) == agentcli.ExitAuthRequired {
				call.grantRequired = g.App
			}
			return "", err
		}
		call.caller = caller
	}
	if call.owner == "" || g.Installations == nil {
		return token, nil
	}
	owners, err := g.Installations.Owners(ctx, token)
	if err != nil {
		return "", err
	}
	for _, owner := range owners {
		if strings.EqualFold(owner, call.owner) {
			return token, nil
		}
	}
	return "", agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
		"the GitHub App %s is not installed on %s: the devctl server acts on GitHub only through that App, so %s's repositories are for devctl on a laptop", g.gitHubApp(), call.owner, call.owner)
}

func (g MusterGrant) gitHubApp() string {
	if g.GitHubApp != "" {
		return g.GitHubApp
	}
	return g.App
}

// Installations answer the accounts a GitHub App is installed on that the
// holder of a user token of it reaches.
type Installations interface {
	Owners(ctx context.Context, token string) ([]string, error)
}

// GitHubInstallations read GET /user/installations at BaseURL and keep each
// token's answer for TTL.
type GitHubInstallations struct {
	BaseURL string
	Client  *http.Client
	TTL     time.Duration
	Now     func() time.Time

	cache tokenCache[[]string]
}

// DefaultInstallationsTTL is how long a token's installations are kept.
const DefaultInstallationsTTL = 5 * time.Minute

// Owners implements [Installations].
func (g *GitHubInstallations) Owners(ctx context.Context, token string) ([]string, error) {
	return g.cache.get(token, g.TTL, DefaultInstallationsTTL, g.Now, func() ([]string, error) { return g.read(ctx, token) })
}

func (g *GitHubInstallations) read(ctx context.Context, token string) ([]string, error) {
	var owners []string
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/user/installations?per_page=100&page=%d", strings.TrimSuffix(g.BaseURL, "/"), page)
		var body struct {
			TotalCount    int `json:"total_count"`
			Installations []struct {
				Account struct {
					Login string `json:"login"`
				} `json:"account"`
			} `json:"installations"`
		}
		if err := getGitHub(ctx, g.Client, url, token, "the App's installations", &body); err != nil {
			return nil, err
		}
		for _, i := range body.Installations {
			owners = append(owners, i.Account.Login)
		}
		if len(body.Installations) < 100 || len(owners) >= body.TotalCount {
			return owners, nil
		}
	}
}

// call is what the server learns about one tool call on its way: the
// person's grants muster attached, the repository's owner, the caller their
// grant names, and the App whose grant the call lacked.
type call struct {
	grants        map[string]string
	owner         string
	caller        Caller
	grantRequired string
}

type callKey struct{}

// callFrom is the call in ctx; a call of its own outside a request.
func callFrom(ctx context.Context) *call {
	if c, ok := ctx.Value(callKey{}).(*call); ok {
		return c
	}
	return &call{}
}

// grantsOf are the provider App tokens of r, by canonical header name.
func grantsOf(r *http.Request) map[string]string {
	grants := map[string]string{}
	for name, values := range r.Header {
		if strings.HasPrefix(name, textproto.CanonicalMIMEHeaderKey(GrantHeaderPrefix)) && len(values) > 0 && values[0] != "" {
			grants[name] = values[0]
		}
	}
	return grants
}
