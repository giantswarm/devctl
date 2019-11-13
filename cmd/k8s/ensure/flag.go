package ensure

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/internal/env"
)

const (
	flagRepo              = "repo"
	flagGithubAccessToken = "github.access.token"
)

type flag struct {
	Repo              string
	GithubAccessToken string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Repo, flagRepo, "", `Repository to check. If not provided, defaults to the current working directory.`)
	cmd.Flags().StringVar(&f.GithubAccessToken, flagGithubAccessToken, os.Getenv(env.DevctlGithubAccessToken), fmt.Sprintf(`Github access token. Defaults to %s environment variable.`, env.DevctlGithubAccessToken))
}

func (f *flag) Validate() error {
	return nil
}
