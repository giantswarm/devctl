package ami

import (
	"github.com/giantswarm/microerror"
)

type Config struct {
	Arch                    string
	Channel                 string
	ChinaBucketName         string
	ChinaBucketRegion       string
	ChinaAWSAccessKeyID     string
	ChinaAWSSecretAccessKey string
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir            string
	MinimumVersion string
	PrimaryDomain  string
	// If KeepExisting is not empty, releases find in the file won't be overridden.
	KeepExisting string
}

func (c *Config) Validate() error {
	if c.Arch == "" {
		return microerror.Maskf(invalidConfigError, "%T.Arch must not be empty", c)
	}
	if c.Channel == "" {
		return microerror.Maskf(invalidConfigError, "%T.Channel must not be empty", c)
	}
	if c.ChinaBucketName == "" {
		return microerror.Maskf(invalidConfigError, "%T.ChinaBucketName must not be empty", c)
	}
	if c.ChinaBucketRegion == "" {
		return microerror.Maskf(invalidConfigError, "%T.ChinaBucketRegion must not be empty", c)
	}
	if c.ChinaAWSAccessKeyID == "" {
		return microerror.Maskf(invalidConfigError, "%T.ChinaAWSAccessKeyID must not be empty", c)
	}
	if c.ChinaAWSSecretAccessKey == "" {
		return microerror.Maskf(invalidConfigError, "%T.ChinaAWSSecretAccessKey must not be empty", c)
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
