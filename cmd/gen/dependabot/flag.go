package dependabot

import (
	"github.com/spf13/cobra"
)

type flag struct {
	Reviewers []string
	Daily     bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringSliceVarP(&f.Reviewers, "reviewers", "r", []string{}, "Reviewers you want to assign automatically when Dependabot creates a PR, e.g. giantswarm/team-firecracker.")
	cmd.Flags().BoolVar(&f.Daily, "daily", false, "Check for updates every day (default: weekly).")
}
