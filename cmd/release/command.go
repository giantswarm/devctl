package release

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "release"
	shortDescription = "Creates new operator release."
	description      = `Replaces version in operator code, normally in pkg/project/project.go.
It will remove the work in progress suffix (-dev) from the version, commit the change and tag that commit to create a Github release from that tag.
Finally, it will increase the patch version and add back the work in progress suffix.`
)

type Config struct {
	Logger micrologger.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	f := &flag{}

	r := &runner{
		flag:   f,
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:   name,
		Short: shortDescription,
		Long:  description,
		RunE:  r.Run,
	}

	f.Init(c)

	return c, nil
}
