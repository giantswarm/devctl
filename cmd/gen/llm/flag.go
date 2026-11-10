package llm

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	flagDryRun              = "dry-run"
	flagFlavour             = "flavour"
	flagLanguage            = "language"
	flagMigrateInstructions = "migrate-instructions"
)

type flag struct {
	DryRun              bool
	Flavours            gen.FlavourSlice
	Language            string
	MigrateInstructions bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`The type of project that you want to generate rules for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", "Language of the repo, for generating additional language-specific rules.")

	// Hidden until gen llm generates AGENTS.md.
	cmd.Flags().BoolVar(&f.DryRun, flagDryRun, false, "Print the preflight findings for AGENTS.md generation without changing anything.")
	cmd.Flags().BoolVar(&f.MigrateInstructions, flagMigrateInstructions, false, "Move existing agent instructions and skills to AGENTS.md and .agents/skills.")
	_ = cmd.Flags().MarkHidden(flagDryRun)
	_ = cmd.Flags().MarkHidden(flagMigrateInstructions)
}

func (f *flag) Validate() error {
	if f.MigrateInstructions && !f.DryRun {
		return microerror.Maskf(invalidFlagError, "--%s is only supported with --%s for now", flagMigrateInstructions, flagDryRun)
	}
	return nil
}
