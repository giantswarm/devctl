package renovate

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagInterval = "interval"
	flagLanguage = "language"
	flagReviewer = "reviewer"
)

type flag struct {
	Interval string
	Language string
	Reviewer string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Interval, flagInterval, "i", "after 6am on thursday", "Check for daily, weekly or monthly updates.")
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", "Language for Renovate to  monitor for new versions , e.g. go, docker.")
	cmd.Flags().StringVarP(&f.Reviewer, flagReviewer, "r", "", "Reviewer you want to assign automatically when Renovate creates a PR, e.g. giantswarm/team-firecracker.")
}

func (f *flag) Validate() error {
	if f.Interval == "" {
		return microerror.Maskf(invalidFlagError, "--%s cannot be empty", flagInterval)
	}
	if f.Language == "" {
		return microerror.Maskf(invalidFlagError, "--%s cannot be empty", flagLanguage)
	}
	return nil
}
