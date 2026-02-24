package create

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	flagName                   = "name"
	flagBase                   = "base"
	flagProvider               = "provider"
	flagComponents             = "component"
	flagApps                   = "app"
	flagOverwrite              = "overwrite"
	flagReleases               = "releases"
	flagBumpAll                = "bumpall"
	flagYes                    = "yes"
	flagDrop                   = "drop"
	flagVerbose                = "verbose"
	flagChangesOnly            = "changes-only"
	flagRequestedOnly          = "requested-only"
	flagPreserveReadme         = "preserve-readme"
	flagRegenerateReadme       = "regenerate-readme"
	flagChangelogNoisePattern  = "changelog-noise-pattern"
)

type flag struct {
	Base                   string
	Apps                   []string
	BumpAll                bool
	Components             []string
	Name                   string
	Overwrite              bool
	Provider               string
	Releases               string
	Yes                    bool
	Drop                   []string
	Output                 string
	Verbose                bool
	ChangesOnly            bool
	RequestedOnly          bool
	UpdateExisting         bool
	PreserveReadme         bool
	RegenerateReadme       bool
	ChangelogNoisePatterns []string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Name, flagName, "", "Name of the new release. Must follow semver format.")
	cmd.Flags().StringVar(&f.Base, flagBase, "", "Existing release upon which to base the new release. Must follow semver format.")
	cmd.Flags().StringVar(&f.Provider, flagProvider, "", "Provider of the release.")
	cmd.Flags().StringSliceVarP(&f.Components, flagComponents, "c", nil, "Updated component version to apply to created release. Can be specified multiple times. Must follow a format of <name>@<version>.")
	cmd.Flags().StringSliceVarP(&f.Apps, flagApps, "a", nil, "Updated app version to apply to created release. Can be specified multiple times. Must follow a format of <name>@<version>[@<component_version>][@<dependencies>].")
	cmd.Flags().BoolVar(&f.Overwrite, flagOverwrite, false, "If true, allow overwriting existing release with the same name.")
	cmd.Flags().StringVar(&f.Releases, flagReleases, ".", "Path to releases repository. Defaults to current working directory.")
	cmd.Flags().BoolVar(&f.BumpAll, flagBumpAll, false, "Bump all components to the latest version.")
	cmd.Flags().BoolVarP(&f.Yes, flagYes, "y", false, "Do not ask for confirmation.")
	cmd.Flags().BoolVar(&f.UpdateExisting, "update-existing", false, "Update an existing release in the current branch instead of creating from a base release.")
	cmd.Flags().StringVar(&f.Output, "output", "text", "Output format (text|markdown).")
	cmd.Flags().BoolVarP(&f.Verbose, flagVerbose, "v", false, "Print verbose output.")
	cmd.Flags().BoolVar(&f.ChangesOnly, flagChangesOnly, false, "Only print changed components and apps.")
	cmd.Flags().BoolVar(&f.RequestedOnly, flagRequestedOnly, false, "Only print components and apps requested by the user.")
	cmd.Flags().StringSliceVar(&f.Drop, flagDrop, nil, "App to drop from the release.")
	cmd.Flags().BoolVar(&f.PreserveReadme, flagPreserveReadme, false, "Preserve existing README.md instead of regenerating it.")
	cmd.Flags().BoolVar(&f.RegenerateReadme, flagRegenerateReadme, false, "When used with --update-existing, regenerate README.md with full changelogs by finding the previous release version.")
	cmd.Flags().StringSliceVar(&f.ChangelogNoisePatterns, flagChangelogNoisePattern, nil, "Changelog entries containing this substring are filtered out. Can be specified multiple times.")
}

func (f *flag) Validate() error {
	if f.Name == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagName)
	}
	if f.Base == "" && !f.UpdateExisting {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty when --update-existing is not used", flagBase)
	}
	if f.Base != "" && f.UpdateExisting {
		return microerror.Maskf(invalidFlagError, "cannot use --%s and --%s at the same time", flagBase, "update-existing")
	}
	if f.Provider == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagProvider)
	}

	return nil
}
