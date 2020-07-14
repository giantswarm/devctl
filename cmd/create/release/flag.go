package release

import (
	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	appFlag       = "app"
	baseFlag      = "base"
	componentFlag = "component"
	nameFlag      = "name"
	overwriteFlag = "overwrite"
	providerFlag  = "provider"
	releasesFlag  = "releases"
)

type flag struct {
	Base       string
	Apps       []string
	Components []string
	Name       string
	Overwrite  bool
	Provider   string
	Releases   string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.Apps, appFlag, nil, `Updated app version to apply to created release. Can be specified multiple times. Must follow a format of <name>@<version>[@<component version>].`)
	cmd.Flags().StringVar(&f.Base, baseFlag, "", `Existing release upon which to base the new release. Must follow semver format.`)
	cmd.Flags().StringArrayVar(&f.Components, componentFlag, nil, `Updated component version to apply to created release. Can be specified multiple times. Must follow a format of <name>@<version>.`)
	cmd.Flags().StringVar(&f.Name, nameFlag, "", `Name of the new release. Must follow semver format.`)
	cmd.Flags().BoolVar(&f.Overwrite, overwriteFlag, false, `If true, allow overwriting existing release with the same name.`)
	cmd.Flags().StringVar(&f.Provider, providerFlag, "", `Target provider for the new release.`)
	cmd.Flags().StringVar(&f.Releases, releasesFlag, ".", `Path to releases repository. Defaults to current working directory.`)
}

func (f *flag) Validate() error {
	if f.Base == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", baseFlag)
	}
	if _, err := semver.NewVersion(f.Base); err != nil {
		return microerror.Maskf(invalidFlagError, "--%s must be a valid semver", baseFlag)
	}
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
