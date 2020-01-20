package replace

import (
	"regexp"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

type flag struct {
	Ignore  []string
	InPlace bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringSliceVar(&f.Ignore, "ignore", []string{}, `Ignore files matching comma separated list of patterns. The pattern recognizes "*", "**" and "?" globing.`)
	cmd.PersistentFlags().BoolVarP(&f.InPlace, "inplace", "i", false, "Write changes to files in-place.")
	cmd.Args = validatePositionalArgs
}

func (f *flag) Validate() error {
	return nil
}

func validatePositionalArgs(cmd *cobra.Command, args []string) error {
	if len(args) < 3 {
		return microerror.Maskf(invalidFlagsError, "expected 3 or more arguments, got %d", len(args))
	}

	if _, err := regexp.Compile(args[0]); err != nil {
		return microerror.Maskf(invalidFlagsError, "first argument is not a valid regex: %v", err)
	}

	return nil
}
