package create

import (
	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	flagApp       = "app"
	flagBase      = "base"
	flagComponent = "component"
	flagName      = "name"
	flagOverwrite = "overwrite"
	flagProvider  = "provider"
	flagReleases  = "releases"
	flagBumpAll   = "bumpall"
)

type flag struct {
	Base       string
	Apps       []string
	BumpAll    bool
	Components []string
	Name       string
	Overwrite  bool
	Provider   string
	Releases   string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.Apps, flagApp, nil, `Updated app version to apply to created release. Can be specified multiple times. Must follow a format of <name>@<version>[@<component version>].`)
	cmd.Flags().StringVar(&f.Base, flagBase, "", `Existing release upon which to base the new release. Must follow semver format.`)
	cmd.Flags().StringArrayVar(&f.Components, flagComponent, nil, `Updated component version to apply to created release. Can be specified multiple times. Must follow a format of <name>@<version>.`)
	cmd.Flags().StringVar(&f.Name, flagName, "", `Name of the new release. Must follow semver format.`)
	cmd.Flags().BoolVar(&f.Overwrite, flagOverwrite, false, `If true, allow overwriting existing release with the same name.`)
	cmd.Flags().BoolVar(&f.BumpAll, flagBumpAll, false, `If true, automatically get a list of updated components and apps.`)
	cmd.Flags().StringVar(&f.Provider, flagProvider, "", `Target provider for the new release.`)
	cmd.Flags().StringVar(&f.Releases, flagReleases, ".", `Path to releases repository. Defaults to current working directory.`)
}

func (f *flag) Validate() error {
	if f.Base == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagBase)
	}
	if _, err := semver.NewVersion(f.Base); err != nil {
		return microerror.Maskf(invalidFlagError, "--%s must be a valid semver", flagBase)
	}
	if f.Name == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagName)
	}
	if _, err := semver.NewVersion(f.Name); err != nil {
		return microerror.Maskf(invalidFlagError, "--%s must be a valid semver", flagName)
	}
	if f.Provider == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagProvider)
	}

	return nil
}
