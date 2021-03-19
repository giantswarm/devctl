package workflows

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
)

const (
	flagCheckSecrets = "check-secrets"
	flagFlavour      = "flavour"
	flagArchitecture = "architecture"
)

type flag struct {
	CheckSecrets  bool
	Flavours      gen.FlavourSlice
	Architectures gen.ArchitectureSlice
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`The type of project that you want to generate the workflows for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().BoolVar(&f.CheckSecrets, flagCheckSecrets, true, "If true, also generate a secret-scanning workflow. Possible values: true (default), false.")
	cmd.Flags().VarP(gen.NewArchitectureSliceFlagValue(&f.Architectures, gen.ArchitectureSlice{gen.ArchitectureDarwin, gen.ArchitectureLinux, gen.ArchitectureDarwinARM64, gen.ArchitectureLinuxARM64}), flagArchitecture, "a", fmt.Sprintf(`The architectures to build release artifacts for through workflows. Possible values: <%s>`, strings.Join(gen.AllArchitectures(), "|")))
}

func (f *flag) Validate() error {
	if len(f.Flavours) == 0 {
		return microerror.Maskf(invalidFlagError, "--%s must be one of: %s", flagFlavour, strings.Join(gen.AllFlavours(), ", "))
	}

	if len(f.Architectures) == 0 {
		return microerror.Maskf(invalidFlagError, "--%s must contain only values of: %s", flagArchitecture, strings.Join(gen.AllArchitectures(), ", "))
	}

	return nil
}
