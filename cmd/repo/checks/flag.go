package checks

import "github.com/spf13/cobra"

type flag struct {
	GithubTokenEnvVar string
	Update            bool
	Checks            []string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environment variable name for Github token.")
	cmd.Flags().BoolVar(&f.Update, "update", false, "Add required status checks on the default branch.")
	cmd.Flags().StringSliceVar(&f.Checks, "checks", nil, "Check names to add to required status checks. Requires --update.")
}

func (f *flag) Validate() error {
	return nil
}
