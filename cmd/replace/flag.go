package replace

import (
	"os"
	"regexp"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

type flag struct {
}

func (f *flag) Init(cmd *cobra.Command) {
}

func (f *flag) Validate() error {
	return nil
}

func validatePositionalArgs(cmd *cobra.Command, args []string) error {
	if len(args) < 3 {
		return microerror.Maskf(invalidFlagsError, "expected 3 arguments, got %d", len(args))
	}

	if _, err := regexp.Compile(args[0]); err != nil {
		return microerror.Maskf(invalidFlagsError, "first argument is not a valid regex: %v", err)
	}

	if _, err := os.Stat(args[2]); err != nil {
		return microerror.Maskf(invalidFlagsError, "cannot access file %#q: %v", args[2], err)
	}

	return nil
}
