package create

import (
	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	flagName       = "name"
	flagBase       = "base"
	flagProvider   = "provider"
	flagComponents = "component"
	flagApps       = "app"
	flagOverwrite  = "overwrite"
	flagReleases   = "releases"
	flagBumpAll    = "bumpall"
	flagYes        = "yes"
	flagDrop       = "drop"
	flagOutput     = "output"
	flagVerbose    = "verbose"
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
	Yes        bool
	Drop       []string
	Output     string
	Verbose    bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.Apps, flagApps, nil, `Updated app version to apply to created release. Can be specified multiple times. Must follow a format of <name>@<version>[@<component version>][@<dependencies>].`)
	cmd.Flags().StringVar(&f.Base, flagBase, "", `Existing release upon which to base the new release. Must follow semver format.`)
	cmd.Flags().StringArrayVar(&f.Components, flagComponents, nil, `Updated component version to apply to created release. Can be specified multiple times. Must follow a format of <name>@<version>.`)
	cmd.Flags().StringVar(&f.Name, flagName, "", `Name of the new release. Must follow semver format.`)
	cmd.Flags().BoolVar(&f.Overwrite, flagOverwrite, false, `If true, allow overwriting existing release with the same name.`)
	cmd.Flags().BoolVar(&f.BumpAll, flagBumpAll, false, `If true, automatically get a list of updated components and apps.`)
	cmd.Flags().StringVar(&f.Provider, flagProvider, "", `Target provider for the new release.`)
	cmd.Flags().StringVar(&f.Releases, flagReleases, ".", `Path to releases repository. Defaults to current working directory.`)
	cmd.Flags().BoolVarP(&f.Yes, flagYes, "y", false, `If true, skip confirmation prompt.`)
	cmd.Flags().StringVar(&f.Output, flagOutput, "text", "Output format (text|markdown).")
	cmd.Flags().BoolVarP(&f.Verbose, flagVerbose, "v", false, "Print verbose output.")
	cmd.Flags().StringArrayVar(&f.Drop, flagDrop, nil, "App to drop from the release.")
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
	if f.Provider != "aws" && f.Provider != "azure" && f.Provider != "vsphere" && f.Provider != "cloud-director" {
		return microerror.Maskf(invalidFlagError, "--%s must be one of 'aws', 'azure', 'vsphere', 'cloud-director'", flagProvider)
	}

	return nil
}
