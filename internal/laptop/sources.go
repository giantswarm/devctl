package laptop

import (
	"context"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// ReleaseWaitSources are the sources of a release wait on a laptop: the
// GitHub token through the gate, the CircleCI token through its gate when
// the tag turns out to carry a CircleCI configuration (its expiry notice to
// warn), and the private registry with the docker keychain, the
// credentials `docker login` stored. The GitHub and CircleCI requests go
// through transport.
func ReleaseWaitSources(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*releasewait.Sources, error) {
	token, err := authstore.RequireGitHub(ctx)
	if err != nil {
		return nil, err
	}
	return releasewait.NewSources(endpoints, transport, releasewait.Credentials{
		GitHubToken: token.Value,
		RenewGitHub: authstore.RenewGitHubToken,
		CircleCI: func(ctx context.Context) (string, string, error) {
			token, err := authstore.RequireCircleCI(ctx)
			if err != nil {
				return "", "", err
			}
			warn(token.Warning)
			return token.Value, token.SourceName(), nil
		},
		Registry: authn.DefaultKeychain,
	})
}

// ReleasePromoteSources are the sources of a promotion on a laptop: the
// GitHub token of [authstore.ResolveGitHub] (the App login, or a token in
// the environment that overrides it; its warning goes to warn), renewed
// only when it is the keychain's. The requests go through transport.
func ReleasePromoteSources(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*releasepromote.Sources, error) {
	token, err := authstore.ResolveGitHub(ctx)
	if err != nil {
		return nil, err
	}
	warn(token.Warning)
	credentials := releasepromote.Credentials{
		GitHubToken:  token.Value,
		Identity:     releasepromote.Identity{Source: token.Source, Login: token.Login},
		NotFoundHint: authstore.GitHubNotFoundHint(token),
	}
	if token.Source == authstore.SourceKeychain {
		credentials.RenewGitHub = authstore.RenewGitHubToken
	}
	return releasepromote.NewSources(endpoints, transport, credentials)
}
