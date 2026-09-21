package reconcile

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/engine"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

type flag struct {
	GithubTokenEnvVar   string
	DispatchTokenEnvVar string
	CircleCITokenEnvVar string
	TeamFile            string
	Team                string
	ComponentType       string
	Schema              string
	Owner               string
	DryRun              bool
	Added               bool
	Steps               []string
	Options             map[string]string
	Output              string
	DevctlAppID         int64
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", engine.DefaultGitHubEnvVar, "Environment variable name for the GitHub token.")
	cmd.Flags().StringVar(&f.DispatchTokenEnvVar, "dispatch-token-envvar", "", "Environment variable name for the GitHub token the catalog step lists and dispatches the catalog and mapping workflow runs with (actions: write on the catalog repository), when the GitHub token's identity holds no Actions permission there. Every read stays with the GitHub token, the repository lookup included. The GitHub token dispatches when unset.")
	cmd.Flags().StringVar(&f.CircleCITokenEnvVar, "circleci-token-envvar", engine.DefaultCircleCIEnvVar, "Environment variable name for the CircleCI token. The CircleCI and release steps are skipped when it is unset.")
	cmd.Flags().StringVar(&f.TeamFile, "team-file", "", "Path of the team file (repositories/<team>.yaml of giantswarm/github) holding the repository's entry; the team is the file's name.")
	cmd.Flags().StringVar(&f.Team, "team", "", "Team slug (team-bumblebee) of a repository without a team-file entry; the entry is then the name alone.")
	cmd.Flags().StringVar(&f.ComponentType, "component-type", "", "Component type of a repository without a team-file entry (service, library, ...).")
	cmd.Flags().StringVar(&f.Schema, "schema", "", "Path of a repositories schema to validate the entry against instead of the one on giantswarm/github main.")
	cmd.Flags().StringVar(&f.Owner, "owner", reposetup.DefaultOwner, "GitHub organisation of a repository given without an owner.")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Check only: print what a repair would change, change nothing.")
	cmd.Flags().BoolVar(&f.Added, "added", false, "The entry was added by the change at hand: a missing repository is created. Never inferred.")
	cmd.Flags().StringSliceVar(&f.Steps, "steps", nil, "Run only these steps (create,scaffold,settings,permissions,protection,circleci,webhooks,renovate,codeowners,metadata,lifecycle,catalog,release); every step when not given.")
	cmd.Flags().StringToStringVar(&f.Options, "option", nil, "Scaffold option as name=value, the template's options; repeatable.")
	cmd.Flags().StringVar(&f.Output, "output", engine.OutputTable, "Output format: table or json.")
	cmd.Flags().Int64Var(&f.DevctlAppID, "devctl-app-id", 0, "Numeric id of the devctl GitHub App (the App's settings page; not the client id): the bypass actor of the default branch's ruleset, in pull_request mode, unless the entry declares agentMerge: false. Unset, the bypass actors are left as they are and the protection step reports the missing configuration.")
}

func (f *flag) Validate() error {
	if f.TeamFile == "" && f.Team == "" {
		return microerror.Maskf(invalidFlagError, "--team-file or --team is required")
	}
	if f.TeamFile != "" && f.Team != "" {
		return microerror.Maskf(invalidFlagError, "--team-file and --team exclude each other: the team is the file's name")
	}
	if f.TeamFile != "" && f.ComponentType != "" {
		return microerror.Maskf(invalidFlagError, "--component-type is for a repository without a team-file entry")
	}
	if f.DevctlAppID < 0 {
		return microerror.Maskf(invalidFlagError, "--devctl-app-id must be a positive App id, got %d", f.DevctlAppID)
	}
	return microerror.Mask(engine.ValidateOutput(f.Output))
}
