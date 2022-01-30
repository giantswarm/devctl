package workflows

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
)

const (
	flagEnableChangelog                = "enable-changelog"
	flagCheckSecrets                   = "check-secrets"
	flagEnableFloatingMajorVersionTags = "enable-floating-major-tags"
	flagFlavour                        = "flavour"
)

type flag struct {
	CheckSecrets                   bool
	EnableChangelog                bool
	EnableFloatingMajorVersionTags bool
	Flavours                       gen.FlavourSlice
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.CheckSecrets, flagCheckSecrets, true, "If true, also generate a secret-scanning workflow. Possible values: true (default), false.")
	cmd.Flags().BoolVar(&f.EnableChangelog, flagEnableChangelog, false, "If true, also generate a changelog automation workflow.")
	cmd.Flags().BoolVar(&f.EnableFloatingMajorVersionTags, flagEnableFloatingMajorVersionTags, false, "If true, also generate steps and workflows to ensure floating major version tags like \"v1\" after the release creation.")
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`The type of project that you want to generate the workflows for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
}

func (f *flag) Validate() error {
	if len(f.Flavours) == 0 {
		return microerror.Maskf(invalidFlagError, "--%s must be one of: %s", flagFlavour, strings.Join(gen.AllFlavours(), ", "))
	}

	return nil
}
