package releasepromote

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// Sources are what one promotion reads and writes, opened after the version
// gate.
type Sources struct {
	GitHub     GitHub
	CheckBuilt func(ctx context.Context, owner, repo, tag string) error
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
	// CircleCI and Registry are the release wait's read credentials.
	CircleCI func(ctx context.Context) (token, source string, err error)
	Registry authn.Keychain
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
	waitSources, err := releasewait.NewSources(endpoints, transport, releasewait.Credentials{
		GitHubToken: credentials.GitHubToken,
		RenewGitHub: credentials.RenewGitHub,
		CircleCI:    credentials.CircleCI,
		Registry:    credentials.Registry,
	})
	if err != nil {
		return nil, err
	}
	return &Sources{
		GitHub: gh,
		CheckBuilt: func(ctx context.Context, owner, repo, tag string) error {
			return checkBuilt(ctx, waitSources, endpoints, agentcli.Clock{}, owner, repo, tag)
		},
		Team: func(ctx context.Context, team string) ([]string, error) {
			return TeamRepositories(ctx, gh.GitHub(), team)
		},
		NotFoundHint: credentials.NotFoundHint,
		Identity:     credentials.Identity,
	}, nil
}

// checkBuilt uses the same artifact derivation and probes as release wait.
// The short wait allows reads to settle without waiting for a candidate build.
func checkBuilt(ctx context.Context, sources *releasewait.Sources, endpoints agentcli.Endpoints, clock agentcli.Clock, owner, repo, tag string) error {
	waiter, err := releasewait.New(releasewait.Config{
		Owner: owner, Repo: repo, Version: tag, Timeout: 30 * time.Second,
		GitHub: sources.GitHub, Entries: sources.Entries, CircleCI: sources.CircleCI,
		Registry: sources.Registry, Endpoints: endpoints, Clock: clock,
	})
	if err != nil {
		return err
	}
	var result releasewait.Result
	if err := waiter.Wait(ctx, &result); err != nil {
		return err
	}
	return verifiedTagPipeline(result)
}
