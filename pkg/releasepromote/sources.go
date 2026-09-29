package releasepromote

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// Sources are what one promotion reads and writes, opened after the version
// gate.
type Sources struct {
	GitHub GitHub
	// Team lists the auto-release repositories of a team.
	Team func(ctx context.Context, team string) ([]string, error)
	// NotFoundHint is [Config]'s.
	NotFoundHint string
	// DispatchBlocked is set when the token cannot dispatch a workflow (the
	// App login, which carries no Actions write): the reason a run that
	// dispatches stops with before any repository is read. Empty when the
	// token may.
	DispatchBlocked string
}

// OpenSources is the production wiring: the GitHub token of
// [authstore.ResolveGitHub] (the App login, or a token in the environment
// that overrides it; its warning goes to warn), conditional requests below
// it, and the team files of the organisation. The requests go through
// transport.
func OpenSources(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*Sources, error) {
	token, err := authstore.ResolveGitHub(ctx)
	if err != nil {
		return nil, err
	}
	warn(token.Warning)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	config := githubclient.Config{Logger: logger, AccessToken: token.Value, BaseURL: endpoints.GitHubAPIURL, Transport: transport}
	if token.Source == authstore.SourceKeychain {
		config.Renew = authstore.RenewGitHubToken
	}
	gh, _, err := githubclient.NewConditional(config)
	if err != nil {
		return nil, err
	}
	return &Sources{
		GitHub: gh,
		Team: func(ctx context.Context, team string) ([]string, error) {
			return TeamRepositories(ctx, gh.GitHub(), team)
		},
		NotFoundHint:    authstore.GitHubNotFoundHint(token),
		DispatchBlocked: DispatchBlockedReason(token),
	}, nil
}

// DispatchBlockedReason is why the App login cannot dispatch a workflow: it
// needs Actions write, which the App does not carry, and the variables that
// override the login. Empty for a token from the environment.
func DispatchBlockedReason(token authstore.Token) string {
	if token.Source != authstore.SourceKeychain {
		return ""
	}
	names := make([]string, len(authstore.GitHubEnvVars))
	for i, name := range authstore.GitHubEnvVars {
		names[i] = "$" + name
	}
	return fmt.Sprintf("dispatching a workflow needs Actions write, which the devctl GitHub App login does not carry: set %s to a token that has it", strings.Join(names, ", "))
}
