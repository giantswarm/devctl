package workflows

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v6/pkg/gen"
)

const (
	flagCheckSecrets                   = "check-secrets"
	flagEnableFloatingMajorVersionTags = "enable-floating-major-tags"
	flagFlavour                        = "flavour"
	flagLanguage                       = "language"
	flagInstallUpdateChart             = "install-update-chart"
	flagRunSecurityScorecard           = "run-security-scorecard"
)

type flag struct {
	CheckSecrets                   bool
	EnableFloatingMajorVersionTags bool
	Flavours                       gen.FlavourSlice
	Language                       string
	InstallUpdateChart             bool
	RunSecurityScorecard           bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.CheckSecrets, flagCheckSecrets, true, "If true, also generate a secret-scanning workflow. Possible values: true (default), false.")
	cmd.Flags().BoolVar(&f.EnableFloatingMajorVersionTags, flagEnableFloatingMajorVersionTags, false, "If true, also generate steps and workflows to ensure floating major version tags like \"v1\" after the release creation.")
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`The type of project that you want to generate the workflows for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", "Language of your repo. If go, also generate a fix_vulnerabilities workflow.")
	cmd.Flags().BoolVar(&f.InstallUpdateChart, flagInstallUpdateChart, false, "If true, also generate update_chart workflow. Only valid for app flavor.")
	cmd.Flags().BoolVar(&f.RunSecurityScorecard, flagRunSecurityScorecard, true, "If true, also generate a security scorecard workflow. Possible values: true (default), false.")
}

func (f *flag) Validate() error {
	if len(f.Flavours) == 0 {
		return microerror.Maskf(invalidFlagError, "--%s must be one of: %s", flagFlavour, strings.Join(gen.AllFlavours(), ", "))
	}

	return nil
}
