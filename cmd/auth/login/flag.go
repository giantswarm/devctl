package login

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type flag struct {
	GitHubOnly   bool
	CircleCIOnly bool
	Progress     bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.GitHubOnly, "github-only", false, "Run the GitHub device flow only")
	cmd.Flags().BoolVar(&f.CircleCIOnly, "circleci-only", false, "Run the CircleCI authorization only")
	agentcli.ProgressFlag(cmd, &f.Progress)
}

func (f *flag) Validate() error {
	if f.GitHubOnly && f.CircleCIOnly {
		return errors.New("--github-only and --circleci-only exclude each other; leave both out for both flows")
	}
	return nil
}
