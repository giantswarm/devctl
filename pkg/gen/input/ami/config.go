package ami

import (
	"github.com/giantswarm/microerror"
)

type Config struct {
	Arch        string
	Channel     string
	ChinaDomain string
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir            string
	MinimumVersion string
	PrimaryDomain  string
}

func (c *Config) Validate() error {
	if c.Arch == "" {
		return microerror.Maskf(invalidConfigError, "%T.Arch must not be empty", c)
	}
	if c.Channel == "" {
		return microerror.Maskf(invalidConfigError, "%T.Channel must not be empty", c)
	}
	if c.ChinaDomain == "" {
		return microerror.Maskf(invalidConfigError, "%T.ChinaDomain must not be empty", c)
	}
	if c.Dir == "" {
		return microerror.Maskf(invalidConfigError, "%T.Dir must not be empty", c)
	}
	if c.MinimumVersion == "" {
		return microerror.Maskf(invalidConfigError, "%T.MinimumVersion must not be empty", c)
	}
	if c.PrimaryDomain == "" {
		return microerror.Maskf(invalidConfigError, "%T.PrimaryDomain must not be empty", c)
	}

	return nil
}
