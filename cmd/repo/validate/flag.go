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
	Mode              string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environment variable name for the GitHub token. Without a token the schema is the embedded copy and repository names are not checked.")
	cmd.Flags().StringVar(&f.TeamFile, "team-file", "", "Path of the team file (repositories/<team>.yaml of giantswarm/github); the team is the file's name.")
	cmd.Flags().StringArrayVar(&f.Entries, "entry", nil, "Name of an entry to validate; repeatable. Every entry of the file when not given.")
	cmd.Flags().StringVar(&f.Mode, "mode", "", "What the entries are validated for: create (schema, creation rules, a free name on GitHub) or existing (schema alone; the name check's verdict is reported, never refuses). Default: create with --entry, whose entries are the ones being added; existing without, the whole file being on main already.")
	cmd.Flags().StringVar(&f.Author, "author", "", "GitHub login of the person opening the change, for the team guard.")
	cmd.Flags().StringArrayVar(&f.AuthorTeams, "author-team", nil, "GitHub team slug the author is a member of (team-bumblebee); repeatable.")
	cmd.Flags().StringVar(&f.Owner, "owner", reposetup.DefaultOwner, "GitHub organisation the repositories are created in.")
	cmd.Flags().StringVar(&f.Schema, "schema", "", "Path of a repositories schema to validate against instead of the one on giantswarm/github main.")
}

func (f *flag) Validate() error {
	if f.TeamFile == "" {
		return microerror.Maskf(invalidFlagError, "--team-file must not be empty")
	}
	switch reposetup.Mode(f.Mode) {
	case "", reposetup.ModeCreate, reposetup.ModeExisting:
	default:
		return microerror.Maskf(invalidFlagError, "--mode %q: want %s or %s", f.Mode, reposetup.ModeCreate, reposetup.ModeExisting)
	}
	return nil
}

// mode is --mode, or the default: the entries named with --entry are being
// added, a whole file is on main already.
func (f *flag) mode() reposetup.Mode {
	switch {
	case f.Mode != "":
		return reposetup.Mode(f.Mode)
	case len(f.Entries) > 0:
		return reposetup.ModeCreate
	}
	return reposetup.ModeExisting
}
