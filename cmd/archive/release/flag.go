package release

import (
	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	nameFlag     = "name"
	providerFlag = "provider"
	releasesFlag = "releases"
)

type flag struct {
	Name     string
	Provider string
	Releases string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Name, nameFlag, "", `Name of the new release. Must follow semver format.`)
	cmd.Flags().StringVar(&f.Provider, providerFlag, "", `Target provider for the to be archived release.`)
	cmd.Flags().StringVar(&f.Releases, releasesFlag, ".", `Path to releases repository. Defaults to current working directory.`)
}

func (f *flag) Validate() error {
	if f.Name == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", nameFlag)
	}
	if _, err := semver.NewVersion(f.Name); err != nil {
		return microerror.Maskf(invalidFlagError, "--%s must be a valid semver", nameFlag)
	}
	if f.Provider == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", providerFlag)
	}

	return nil
}
