package depclient

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	toml "github.com/pelletier/go-toml"
)

type Config struct {
	Logger micrologger.Logger
}

type Client struct {
	logger micrologger.Logger
}

func New(config Config) (*Client, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}

	c := &Client{
		logger: config.Logger,
	}

	return c, nil
}

// ReadManifest unmarshals content of Gopkg.toml.
func (c *Client) ReadManifest(ctx context.Context, data []byte) (Manifest, error) {
	var m Manifest

	err := toml.Unmarshal(data, &m)
	if err != nil {
		return Manifest{}, microerror.Mask(err)
	}

	return m, err
}
