package githubclient

import (
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// NewConditional is New for a poll loop: every GET is a conditional request
// through a [Conditional], whose Rate the caller reads for its interval. The
// client is read-only in practice (DryRun is not applied) and config.BaseURL
// is used as given, with no /api/v3/ suffix: it names the REST API root
// itself (api.github.com, or a test double serving /repos/... at its root).
func NewConditional(config Config) (*Client, *Conditional, error) {
	baseURL := config.BaseURL
	config.BaseURL = ""
	config.DryRun = false
	c, err := New(config)
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}

	httpClient := c.ghClient.Client()
	conditional := &Conditional{Base: httpClient.Transport}
	httpClient.Transport = conditional
	opts := []github.ClientOptionsFunc{github.WithHTTPClient(httpClient)}
	if baseURL != "" {
		base := strings.TrimRight(baseURL, "/") + "/"
		opts = append(opts, github.WithURLs(&base, nil))
	}
	ghClient, err := github.NewClient(opts...)
	if err != nil {
		return nil, nil, microerror.Maskf(invalidConfigError, "%T.BaseURL: %v", config, err)
	}
	c.ghClient = ghClient
	return c, conditional, nil
}
