package checks

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/engine"
)

type flag struct {
	GithubTokenEnvVar string
	Update            bool
	Checks            []string
	ChecksIfReported  []string
	Remove            []string
	CircleCIDir       string
	Output            string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", engine.DefaultGitHubEnvVar, "Environment variable name for Github token.")
	cmd.Flags().BoolVar(&f.Update, "update", false, "Update the required status checks on the default branch; without it the drift is printed and nothing changes.")
	cmd.Flags().StringSliceVar(&f.Checks, "checks", nil, "Check names required whatever has reported.")
	cmd.Flags().StringSliceVar(&f.ChecksIfReported, "checks-if-reported", nil, "Check names required once they have reported on the default branch or a recently merged pull request.")
	cmd.Flags().StringSliceVar(&f.Remove, "remove", nil, "Check names never required: removed when present.")
	cmd.Flags().StringVar(&f.CircleCIDir, "circleci-dir", "", "Path of the repository's .circleci directory as just generated, read instead of the repository's: its branch-side jobs (workflows.yml plus custom.yml, all workflows; a job counts unless its branch filter has `only:` or ignores every branch) are required as 'ci/circleci: <job>' once they have reported, and every required 'ci/circleci:' context without such a job is removed.")
	cmd.Flags().StringVar(&f.Output, "output", engine.OutputTable, "Output format: table or json.")
}

func (f *flag) Validate() error {
	return microerror.Mask(engine.ValidateOutput(f.Output))
}
