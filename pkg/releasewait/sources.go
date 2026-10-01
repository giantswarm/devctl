package releasewait

import (
	"context"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
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
}

// OpenSources is the production wiring: the GitHub token through the gate,
// conditional requests below it, the team files of the organisation, the
// CircleCI token through its gate when the tag turns out to carry a
// CircleCI configuration, the registries and the catalog index. The GitHub
// and CircleCI requests go through transport.
func OpenSources(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*Sources, error) {
	token, err := authstore.RequireGitHub(ctx)
	if err != nil {
		return nil, err
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	gh, conditional, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: token.Value, BaseURL: endpoints.GitHubAPIURL, Transport: transport, Renew: authstore.RenewGitHubToken})
	if err != nil {
		return nil, err
	}
	return &Sources{
		GitHub:  gh,
		Entries: TeamFileEntries{GitHub: gh.GetUnderlyingClient(ctx)},
		CircleCI: func(ctx context.Context) (CircleCI, error) {
			token, err := authstore.RequireCircleCI(ctx)
			if err != nil {
				return nil, err
			}
			warn(token.Warning)
			client, err := circleciclient.New(circleciclient.Config{Token: token.Value, BaseURL: circleciclient.BaseURLFromAPIURL(endpoints.CircleCIAPIURL), HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				return nil, err
			}
			return client, nil
		},
		Registry:     RegistryProber{Endpoints: endpoints},
		CatalogIndex: CatalogIndex{BaseURL: CatalogURLFromEnv()},
		Rate:         conditional.Rate,
	}, nil
}
