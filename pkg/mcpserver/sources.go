package mcpserver

import (
	"context"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// PRWaitSources are what a pull request wait reads.
type PRWaitSources struct {
	GitHub  prwait.GitHub
	Rate    prwait.RateSource
	Entries prwait.EntryFinder
}

// Sources build the clients of one call from the person's token; the
// requests go through transport.
type Sources interface {
	PRWait(ctx context.Context, token string, transport http.RoundTripper) (PRWaitSources, error)
	ReleaseWait(ctx context.Context, token string, transport http.RoundTripper) (*releasewait.Sources, error)
}

// GitHubSources are the clients of GitHub and the registries at Endpoints.
// They hold no CircleCI client: the server has no CircleCI credential, so a
// pull request's CircleCI state is the commit statuses CircleCI posts to
// GitHub. The private registry is read anonymously.
type GitHubSources struct {
	Endpoints agentcli.Endpoints
}

// PRWait implements [Sources].
func (g GitHubSources) PRWait(_ context.Context, token string, transport http.RoundTripper) (PRWaitSources, error) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	github, conditional, err := githubclient.NewConditional(githubclient.Config{
		Logger:      logger,
		AccessToken: token,
		BaseURL:     g.Endpoints.GitHubAPIURL,
		Transport:   transport,
	})
	if err != nil {
		return PRWaitSources{}, err
	}
	return PRWaitSources{GitHub: github, Rate: conditional, Entries: releasewait.TeamFileEntries{GitHub: github.GitHub()}}, nil
}

// ReleaseWait implements [Sources].
func (g GitHubSources) ReleaseWait(_ context.Context, token string, transport http.RoundTripper) (*releasewait.Sources, error) {
	sources, err := releasewait.NewSources(g.Endpoints, transport, releasewait.Credentials{GitHubToken: token})
	if err != nil {
		return nil, err
	}
	sources.CircleCI = noCircleCI
	return sources, nil
}

// noCircleCI is the CircleCI of a server that holds no CircleCI credential:
// a release whose tag pipeline runs on CircleCI cannot be judged by it yet.
func noCircleCI(context.Context) (releasewait.CircleCI, error) {
	return nil, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage,
		"the release's pipeline runs on CircleCI and the devctl server reads no CircleCI: wait for it with devctl release wait on a laptop")
}
