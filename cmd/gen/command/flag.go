package command

import (
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagDir  = "dir"
	flagName = "name"
)

type flag struct {
	Dir  string
	Name string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Dir, flagDir, "d", "", `Relative command directory/package. Must start with "cmd".`)
	cmd.Flags().StringVarP(&f.Name, flagName, "n", "", `CLI command binary name.`)
}

func (f *flag) Validate() error {
	if f.Dir == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDir)
	}
	if f.Name == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagName)
	}

	if f.Dir != "cmd" && !strings.HasPrefix(f.Dir, "cmd/") {
		return microerror.Maskf(invalidFlagError, "--%s must value must start with %q", flagDir, "cmd")
	}

	return nil
}
