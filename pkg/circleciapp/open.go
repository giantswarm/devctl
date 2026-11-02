package circleciapp

import (
	"context"
	"net/http"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/rerun"
)

// OpenAsPerson builds the two clients Setup takes from the keychain: GitHub
// on the identity devctl pr wait picks for owner (the devctl App login for
// giantswarm, the person's gh login for every other owner), CircleCI on the
// person's devctl CircleCI login. warn receives the login's expiry warning.
func OpenAsPerson(ctx context.Context, owner string, warn func(string)) (GitHub, CircleCI, error) {
	endpoints := agentcli.EndpointsFromEnv()
	gh, _, err := rerun.NewGitHub(ctx, owner, authstore.RequireGitHub, authexec.PersonGitHub, endpoints, http.DefaultTransport)
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}
	ci, err := rerun.NewCircleCI(ctx, authstore.RequireCircleCI, endpoints, http.DefaultTransport, warn)
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}
	return NewGitHub(gh.GetUnderlyingClient(ctx)), ci, nil
}
