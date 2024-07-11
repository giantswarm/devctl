package githubclient

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v63/github"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

type Config struct {
	DryRun bool
	Logger *logrus.Logger

	AccessToken string
}

type Client struct {
	dryRun bool
	logger *logrus.Logger

	accessToken string
}

func New(config Config) (*Client, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}

	if config.AccessToken == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.AccessToken must not be empty", config)
	}

	c := &Client{
		dryRun: config.DryRun,
		logger: config.Logger,

		accessToken: config.AccessToken,
	}

	return c, nil
}

func (c *Client) getUnderlyingClient(ctx context.Context) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{
			AccessToken: c.accessToken,
		},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	return client
}
