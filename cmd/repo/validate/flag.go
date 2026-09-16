package validate

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

type flag struct {
	GithubTokenEnvVar string
	TeamFile          string
	Entries           []string
	Author            string
	AuthorTeams       []string
	Owner             string
	Schema            string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environment variable name for the GitHub token. Without a token the schema is the embedded copy and repository names are not checked.")
	cmd.Flags().StringVar(&f.TeamFile, "team-file", "", "Path of the team file (repositories/<team>.yaml of giantswarm/github); the team is the file's name.")
	cmd.Flags().StringArrayVar(&f.Entries, "entry", nil, "Name of an entry being added, the ones the creation rules apply to; repeatable. Every entry of the file when not given.")
	cmd.Flags().StringVar(&f.Author, "author", "", "GitHub login of the person opening the change, for the team guard.")
	cmd.Flags().StringArrayVar(&f.AuthorTeams, "author-team", nil, "GitHub team slug the author is a member of (team-bumblebee); repeatable.")
	cmd.Flags().StringVar(&f.Owner, "owner", reposetup.DefaultOwner, "GitHub organisation the repositories are created in.")
	cmd.Flags().StringVar(&f.Schema, "schema", "", "Path of a repositories schema to validate against instead of the one on giantswarm/github main.")
}

func (f *flag) Validate() error {
	if f.TeamFile == "" {
		return microerror.Maskf(invalidFlagError, "--team-file must not be empty")
	}
	return nil
}
