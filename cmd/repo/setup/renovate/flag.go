package renovate

import "github.com/spf13/cobra"

type flag struct {
	GithubTokenEnvVar string
	Remove            bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.Remove, "remove", false, "Set this flag to remove Renovate with the repo. Otherwise it will be added.")
	//cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environment variable name for Github token.")
}

func (f *flag) Validate() error {
	return nil
}
