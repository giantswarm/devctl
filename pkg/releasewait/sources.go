package releasewait

import (
	"context"
	"io"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// Sources are what one wait reads, opened after the version gate: the
// fields of [Config] that are not the wait's own parameters.
type Sources struct {
	GitHub       GitHub
	Entries      EntryFinder
	CircleCI     func(ctx context.Context) (CircleCI, error)
	Registry     Prober
	CatalogIndex CatalogReader
	Rate         func() githubclient.RateLimit
	// Client is the GitHub client behind GitHub, for what a rollout wait
	// reads beyond the release (a configuration change's files).
	Client *githubclient.Client
}

// Credentials are what a wait acts with, handed in by its caller: the CLI
// reads them from the keychain, a server holds them for the person it
// serves. Nothing in this package reads a credential store.
type Credentials struct {
	// GitHubToken is the GitHub access token; required.
	GitHubToken string
	// RenewGitHub is what a GitHub client that got 401 for the rejected
	// token sends its request with again; nil never renews.
	RenewGitHub func(ctx context.Context, rejected string) (string, error)
	// CircleCI answers the CircleCI token and where it came from, asked for
	// only when the tag turns out to carry a CircleCI configuration; nil is
	// exit 8 then.
	CircleCI func(ctx context.Context) (token, source string, err error)
	// Registry answers the private registry; nil reads it anonymously.
	Registry authn.Keychain
}

// NewSources builds the sources of a wait from credentials: the GitHub
// client with conditional requests below it, the team files of the
// organisation, the CircleCI client when first needed, the registries and
// the catalog index. The GitHub and CircleCI requests go through transport.
func NewSources(endpoints agentcli.Endpoints, transport http.RoundTripper, credentials Credentials) (*Sources, error) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	gh, conditional, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: credentials.GitHubToken, BaseURL: endpoints.GitHubAPIURL, Transport: transport, Renew: credentials.RenewGitHub})
	if err != nil {
		return nil, err
	}
	return &Sources{
		GitHub:  gh,
		Entries: TeamFileEntries{GitHub: gh.GitHub()},
		CircleCI: func(ctx context.Context) (CircleCI, error) {
			if credentials.CircleCI == nil {
				return nil, agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired, "the release's pipeline runs on CircleCI and no CircleCI token was handed in")
			}
			token, source, err := credentials.CircleCI(ctx)
			if err != nil {
				return nil, err
			}
			client, err := circleciclient.New(circleciclient.Config{Token: token, TokenSource: source, BaseURL: circleciclient.BaseURLFromAPIURL(endpoints.CircleCIAPIURL), HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				return nil, err
			}
			return client, nil
		},
		Registry:     RegistryProber{Endpoints: endpoints, Keychain: credentials.Registry},
		CatalogIndex: CatalogIndex{BaseURL: CatalogURLFromEnv()},
		Rate:         conditional.Rate,
		Client:       gh,
	}, nil
}
