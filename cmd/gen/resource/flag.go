package resource

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	flagDir               = "dir"
	flagObjectFullType    = "object-full-type"
	flagObjectImportAlias = "object-import-alias"
	flagStateFullType     = "state-full-type"
	flagStateImportAlias  = "state-import-alias"
)

type flag struct {
	Dir               string
	ObjectFullType    string
	ObjectImportAlias string
	StateFullType     string
	StateImportAlias  string
}

func (f *flag) Init(cmd *cobra.Command) {
	// TODO update descs
	cmd.Flags().StringVar(&f.Dir, flagDir, "", `Directory/package where the generated code should be located.`)
	cmd.Flags().StringVar(&f.ObjectFullType, flagObjectFullType, "", ``)
	cmd.Flags().StringVar(&f.ObjectImportAlias, flagObjectImportAlias, "", ``)
	cmd.Flags().StringVar(&f.StateFullType, flagStateFullType, "", ``)
	cmd.Flags().StringVar(&f.StateImportAlias, flagStateImportAlias, "", ``)
}

func (f *flag) Validate() error {
	if f.Dir == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDir)
	}
	if f.ObjectFullType == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagObjectFullType)
	}
	if f.StateFullType == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagStateFullType)
	}

	return nil
}
