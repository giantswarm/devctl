package checks

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

type flag struct {
	GithubTokenEnvVar string
	Update            bool
	Checks            []string
	ChecksIfReported  []string
	Remove            []string
	CircleCIDir       string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environment variable name for Github token.")
	cmd.Flags().BoolVar(&f.Update, "update", false, "Update required status checks on the default branch.")
	cmd.Flags().StringSliceVar(&f.Checks, "checks", nil, "Check names to add to required status checks. Requires --update.")
	cmd.Flags().StringSliceVar(&f.ChecksIfReported, "checks-if-reported", nil, "Check names to add to required status checks only if they have reported on the default branch or a recently merged pull request; the others are skipped. Requires --update.")
	cmd.Flags().StringSliceVar(&f.Remove, "remove", nil, "Check names to remove from required status checks. Requires --update.")
	cmd.Flags().StringVar(&f.CircleCIDir, "circleci-dir", "", "Path to the repository's .circleci directory. Its branch-side jobs (workflows.yml plus custom.yml, all workflows; a job counts unless its branch filter has `only:` or ignores every branch) are required as 'ci/circleci: <job>' once they have reported, and every required 'ci/circleci:' context without such a job is removed. Requires --update.")
}

func (f *flag) Validate() error {
	if f.CircleCIDir != "" && !f.Update {
		return microerror.Maskf(invalidFlagError, "--circleci-dir requires --update")
	}
	return nil
}
