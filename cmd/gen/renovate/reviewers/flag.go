package reviewers

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagReviewers = "reviewers"
)

type flag struct {
	Reviewers []string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringSliceVarP(&f.Reviewers, flagReviewers, "r", []string{}, "Reviewers to set in the Renovate config, e.g. team:team-rocket. Repeat or comma-separate for multiple.")
}

func (f *flag) Validate() error {
	if len(f.Reviewers) == 0 {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagReviewers)
	}
	return nil
}
