package makefile

import (
	"github.com/giantswarm/microerror"
)

type Config struct {
	Dir         string
	Application string
}

func (c *Config) Validate() error {
	if c.Application == "" {
		return microerror.Maskf(invalidConfigError, "%T.Application must not be empty", c)
	}

	return nil
}
