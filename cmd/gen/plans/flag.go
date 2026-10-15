package plans

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	flagFlavour = "flavour"
)

type flag struct {
	Flavours gen.FlavourSlice
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`The type of project. Files are generated only for the plans flavour. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
}

func (f *flag) Validate() error {
	return nil
}
