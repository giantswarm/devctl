package cmd

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

type flag struct {
}

func (f *flag) Init(cmd *cobra.Command, args []string) error {
	var err error

	err = f.init(cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	err = f.validate()
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (f *flag) init(cmd *cobra.Command, args []string) error {
	return nil
}

func (f *flag) validate() error {
	return nil
}
