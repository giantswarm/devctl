package find

import "github.com/spf13/cobra"

type flag struct {
	What               []string
	MustHaveCodeowners bool

	IncludeArchived bool
	IncludeFork     bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&f.IncludeArchived, "include-archived", false, "Also return archived repositories.")
	cmd.PersistentFlags().BoolVar(&f.IncludeFork, "include-fork", false, "Also return giantswarm forks of repositories.")
	cmd.PersistentFlags().StringSliceVar(&f.What, "what", []string{}, "What repos to find. See full help for all available criteria.")
	cmd.PersistentFlags().BoolVar(&f.MustHaveCodeowners, "must-have-codeowners", false, "Only return repositories that have CODEOWNERS.")
}

func (f *flag) Validate() error {
	return nil
}
