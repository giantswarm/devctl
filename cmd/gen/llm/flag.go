package llm

import (
	"fmt"
	"strings"

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
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`The type of project. Nothing is generated for fork. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", "Language of the repo, for language-specific instructions.")
	cmd.Flags().BoolVar(&f.DryRun, flagDryRun, false, "Print the changes or the findings without changing anything.")
	cmd.Flags().BoolVar(&f.MigrateInstructions, flagMigrateInstructions, false, "Also move existing agent instructions to AGENTS.md and skills to .agents/skills.")
}

func (f *flag) Validate() error {
	return nil
}
