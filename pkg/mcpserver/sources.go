package mcpserver

import (
	"context"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// PRWaitSources are what a pull request wait reads.
type PRWaitSources struct {
	GitHub  prwait.GitHub
	Rate    prwait.RateSource
	Entries prwait.EntryFinder
}

// PRMergeSources are what a merge reads and writes, its release wait
// aside, which reads [Sources.ReleaseWait]'s.
type PRMergeSources struct {
	GitHub  prmerge.GitHub
	Rate    prwait.RateSource
	Entries prwait.EntryFinder
	// Policy is the repository's opt-out; nil reads the team files through
	// GitHub, a *githubclient.Client.
	Policy prmerge.Policy
}

// Sources build the clients of one call from the person's token; the
// requests go through transport.
type Sources interface {
	PRWait(ctx context.Context, token string, transport http.RoundTripper) (PRWaitSources, error)
	PRMerge(ctx context.Context, token string, transport http.RoundTripper) (PRMergeSources, error)
	ReleaseWait(ctx context.Context, token string, transport http.RoundTripper) (*releasewait.Sources, error)
	ReleasePromote(ctx context.Context, token string, transport http.RoundTripper) (*releasepromote.Sources, error)
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
	github, conditional, err := g.client(token, transport)
	if err != nil {
		return PRWaitSources{}, err
	}
	return PRWaitSources{GitHub: github, Rate: conditional, Entries: releasewait.TeamFileEntries{GitHub: github.GitHub()}}, nil
}

// PRMerge implements [Sources].
func (g GitHubSources) PRMerge(_ context.Context, token string, transport http.RoundTripper) (PRMergeSources, error) {
	github, conditional, err := g.client(token, transport)
	if err != nil {
		return PRMergeSources{}, err
	}
	return PRMergeSources{GitHub: github, Rate: conditional, Entries: releasewait.TeamFileEntries{GitHub: github.GitHub()}}, nil
}

// ReleasePromote implements [Sources]: the person's token, never renewed.
func (g GitHubSources) ReleasePromote(_ context.Context, token string, transport http.RoundTripper) (*releasepromote.Sources, error) {
	return releasepromote.NewSources(g.Endpoints, transport, releasepromote.Credentials{
		GitHubToken: token,
		Identity:    releasepromote.Identity{Source: IdentitySource},
	})
}

// IdentitySource is a promotion's identity source on the server: the token
// the platform handed over for the person.
const IdentitySource = "platform"

// client is the GitHub client of token with conditional requests below it.
func (g GitHubSources) client(token string, transport http.RoundTripper) (*githubclient.Client, *githubclient.Conditional, error) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return githubclient.NewConditional(githubclient.Config{
		Logger:      logger,
		AccessToken: token,
		BaseURL:     g.Endpoints.GitHubAPIURL,
		Transport:   transport,
	})
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
