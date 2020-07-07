package dependabot

import (
	"github.com/spf13/cobra"
)

type flag struct {
	Reviewers []string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Reviewers, "reviewers", "", "Reviewers you want to assign automatically when Dependabot creates a PR, comma separated.")
}
