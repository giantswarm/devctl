package deploy

import (
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

func newGitHubClient(token authstore.Token) (*githubclient.Client, error) {
	config := githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token.Value,
	}
	// An explicit environment token must never fall back to the App login.
	if token.Source == authstore.SourceKeychain {
		config.Renew = authstore.RenewGitHubToken
	}
	return githubclient.New(config)
}
