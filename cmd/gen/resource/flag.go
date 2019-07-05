package resource

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	flagDir           = "dir"
	flagObjectGroup   = "object.group"
	flagObjectKind    = "object.kind"
	flagObjectVersion = "object.version"
)

type flag struct {
	Dir           string
	ObjectGroup   string
	ObjectKind    string
	ObjectVersion string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Dir, flagDir, "", `Directory/package where the generated code should be located.`)
	cmd.Flags().StringVar(&f.ObjectGroup, flagObjectGroup, "", `Group of the object reconciled by the resource, e.g. "apps".`)
	cmd.Flags().StringVar(&f.ObjectKind, flagObjectKind, "", `Kind of the object reconciled by the resource, e.g. "Deployment".`)
	cmd.Flags().StringVar(&f.ObjectVersion, flagObjectVersion, "", `Kind of the object reconciled by the resource, e.g. "v1".`)
}

func (f *flag) Validate() error {
	if f.Dir == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDir)
	}
	if f.ObjectGroup == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagObjectGroup)
	}
	if f.ObjectKind == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagObjectKind)
	}
	if f.ObjectVersion == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagObjectVersion)
	}

	return nil
}
