package update

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "update"
	shortDescription = `Update the application to the newest version available.`
	longDescription  = `Update the application to the newest version available.

The auto-updater will automatically fetch the newest version archive from the GitHub release. It will then unarchive it, and replace the binary that is currently installed with the one from the archive.

Release binaries are signed in CI (cosign, keyless) and published next to their Sigstore bundle. The downloaded binary is installed only after that bundle verifies for a CircleCI build of giantswarm/devctl; a release without a bundle, or a download that does not match its signature, is refused and the installed binary stays as it is.`
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
		Aliases: []string{"upgrade"},
		Use:     name,
		Short:   shortDescription,
		Long:    longDescription,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
