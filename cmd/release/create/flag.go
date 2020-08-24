package create

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	flagBase      = "base"
	flagOverwrite = "overwrite"
	flagPatch     = "patch"
	flagProvider  = "provider"
	flagReleases  = "releases"
)

type flag struct {
	Base      string
	Overwrite bool
	Patch     string
	Provider  string
	Releases  string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Base, flagBase, "", `Name of the new release. Must follow semver format.`)
	cmd.Flags().BoolVar(&f.Overwrite, flagOverwrite, false, `If true, allow overwriting existing release with the same name.`)
	cmd.Flags().StringVar(&f.Patch, flagPatch, "", `Name of the new release. Must follow semver format.`)
	cmd.Flags().StringVar(&f.Provider, flagProvider, "", `Target provider for the new release.`)
	cmd.Flags().StringVar(&f.Releases, flagReleases, ".", `Path to releases repository. Defaults to current working directory.`)
}

func (f *flag) Validate() error {
	if f.Base == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagBase)
	}
	if f.Patch == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagPatch)
	}
	if f.Provider == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagProvider)
	}

	return nil
}
