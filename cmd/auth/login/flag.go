package login

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type flag struct {
	GitHubOnly     bool
	CircleCIOnly   bool
	MusterOnly     bool
	MusterEndpoint string
	Progress       bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.GitHubOnly, "github-only", false, "Run the GitHub device flow only")
	cmd.Flags().BoolVar(&f.CircleCIOnly, "circleci-only", false, "Run the CircleCI authorization only")
	cmd.Flags().BoolVar(&f.MusterOnly, "muster-only", false, "Run the muster sign-in only: the OAuth flow of the muster endpoint, then the sign-in to giantswarm-repo-manager (the GitHub App consent, once), for the repo commands")
	cmd.Flags().StringVar(&f.MusterEndpoint, "muster-endpoint", agentcli.EndpointsFromEnv().MusterURL, "The muster MCP endpoint --muster-only signs in to ($"+agentcli.EnvMusterURL+")")
	agentcli.ProgressFlag(cmd, &f.Progress)
}

func (f *flag) Validate() error {
	only := 0
	for _, set := range []bool{f.GitHubOnly, f.CircleCIOnly, f.MusterOnly} {
		if set {
			only++
		}
	}
	if only > 1 {
		return errors.New("--github-only, --circleci-only and --muster-only exclude each other; leave them out for the GitHub and CircleCI flows")
	}
	if f.MusterOnly && f.MusterEndpoint == "" {
		return errors.New("--muster-endpoint must name the muster MCP endpoint")
	}
	return nil
}
