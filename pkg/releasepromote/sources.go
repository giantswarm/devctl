package releasepromote

import (
	"context"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
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
	// Identity is who the token acts as, for the document.
	Identity Identity
}

// Credentials are what a promotion acts with, handed in by its caller: the
// CLI reads them from the keychain or the environment, a server holds them
// for the person it serves. Nothing in this package reads a credential
// store.
type Credentials struct {
	// GitHubToken is the GitHub access token; required.
	GitHubToken string
	// RenewGitHub is what a GitHub client that got 401 for the rejected
	// token sends its request with again; nil never renews.
	RenewGitHub func(ctx context.Context, rejected string) (string, error)
	// Identity is who the token acts as, for the document.
	Identity Identity
	// NotFoundHint is [Config]'s: what GitHub's 404 for a repository the
	// token cannot read adds.
	NotFoundHint string
}

// NewSources builds the sources of a promotion from credentials: the GitHub
// client with conditional requests below it and the team files of the
// organisation. The requests go through transport.
func NewSources(endpoints agentcli.Endpoints, transport http.RoundTripper, credentials Credentials) (*Sources, error) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	gh, _, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: credentials.GitHubToken, BaseURL: endpoints.GitHubAPIURL, Transport: transport, Renew: credentials.RenewGitHub})
	if err != nil {
		return nil, err
	}
	return &Sources{
		GitHub: gh,
		Team: func(ctx context.Context, team string) ([]string, error) {
			return TeamRepositories(ctx, gh.GitHub(), team)
		},
		NotFoundHint: credentials.NotFoundHint,
		Identity:     credentials.Identity,
	}, nil
}
