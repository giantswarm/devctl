package validate

import (
	"os"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
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
	Local             bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "", "With --local: environment variable holding a GitHub token that overrides the devctl GitHub App login; empty means $DEVCTL_GITHUB_TOKEN, $GITHUB_TOKEN or $OPSCTL_GITHUB_TOKEN. Without a token the schema is the embedded copy and repository names are not checked.")
	cmd.Flags().StringVar(&f.TeamFile, "team-file", "", "Path of the team file (repositories/<team>.yaml of giantswarm/github); the team is the file's name.")
	cmd.Flags().StringArrayVar(&f.Entries, "entry", nil, "Name of an entry to validate; repeatable. Every entry of the file when not given.")
	cmd.Flags().StringVar(&f.Mode, "mode", "", "What the entries are validated for: create (schema, creation rules, a free name on GitHub) or existing (schema alone; the name check's verdict is reported, never refuses; devctl's own engine, --local). Default: create with --entry, whose entries are the ones being added; existing without, the whole file being on main already.")
	cmd.Flags().StringVar(&f.Author, "author", "", "With --local: GitHub login of the person opening the change, for the team guard. The manager reads it from your sign-in.")
	cmd.Flags().StringArrayVar(&f.AuthorTeams, "author-team", nil, "With --local: GitHub team slug the author is a member of (team-bumblebee); repeatable.")
	cmd.Flags().StringVar(&f.Owner, "owner", reposetup.DefaultOwner, "GitHub organisation the repositories are created in; another than "+reposetup.DefaultOwner+" takes --local.")
	cmd.Flags().StringVar(&f.Schema, "schema", "", "With --local: path of a repositories schema to validate against instead of the one on giantswarm/github main.")
	cmd.Flags().BoolVar(&f.Local, "local", false, "Validate with devctl's own engine and schema instead of asking "+manager.Server+"; the output's engine says which answered.")
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

// localReason says why devctl's own engine validates instead of
// giantswarm-repo-manager -- --local, or CI, where the keychain is never
// read -- and is empty when the manager answers. What only the local engine
// does is refused without either, never run locally in the manager's place:
// existing mode, --schema, --author and --author-team, another --owner. The
// manager validates declarations being added to a team file of the org, as
// the person signed in.
func (f *flag) localReason() (string, error) {
	switch {
	case f.Local:
		return "--local", nil
	case os.Getenv(authstore.EnvCI) != "":
		return "$" + authstore.EnvCI + " is set, so the keychain is not read", nil
	}
	switch {
	case f.mode() == reposetup.ModeExisting:
		return "", microerror.Maskf(invalidFlagError, "existing mode is the local engine's: pass --local; %s validates declarations being added (create mode, --entry)", manager.Server)
	case f.Schema != "":
		return "", microerror.Maskf(invalidFlagError, "--schema is the local engine's: pass --local; %s validates against the schema on %s/%s@%s", manager.Server, reposetup.SchemaRepositoryOwner, reposetup.SchemaRepository, reposetup.SchemaRef)
	case f.Author != "" || len(f.AuthorTeams) > 0:
		return "", microerror.Maskf(invalidFlagError, "--author and --author-team are the local engine's: pass --local; %s reads the author from your sign-in", manager.Server)
	case f.Owner != "" && f.Owner != reposetup.DefaultOwner:
		return "", microerror.Maskf(invalidFlagError, "--owner %s is the local engine's: pass --local; %s validates declarations of %s", f.Owner, manager.Server, reposetup.DefaultOwner)
	}
	return "", nil
}
