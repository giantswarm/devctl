package status

import (
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const (
	outputText = "text"
	outputJSON = "json"
)

type flag struct {
	GithubTokenEnvVar   string
	CircleCITokenEnvVar string
	MusterEndpoint      string
	MusterTokenEnvVar   string
	Team                string
	Owner               string
	Output              string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environment variable holding your GitHub token; the gh CLI's login when it is unset.")
	cmd.Flags().StringVar(&f.CircleCITokenEnvVar, "circleci-token-envvar", "CIRCLECI_TOKEN", "Environment variable holding your CircleCI token; without one the CircleCI and release steps are skipped.")
	cmd.Flags().StringVar(&f.MusterEndpoint, "muster-endpoint", os.Getenv("MUSTER_ENDPOINT"), "Muster MCP endpoint giantswarm-repo-manager is reached through ($MUSTER_ENDPOINT); empty runs the engine's checks locally.")
	cmd.Flags().StringVar(&f.MusterTokenEnvVar, "muster-token-envvar", "MUSTER_TOKEN", "Environment variable holding your bearer token for the muster endpoint.")
	cmd.Flags().StringVar(&f.Team, "team", "", "Team whose file declares the repository (bumblebee or team-bumblebee); every team file is searched when unset.")
	cmd.Flags().StringVar(&f.Owner, "owner", reposetup.DefaultOwner, "GitHub organisation of the repository when the argument carries none.")
	cmd.Flags().StringVarP(&f.Output, "output", "o", outputText, "Output format: text or json.")
}

func (f *flag) Validate() error {
	if f.Output != outputText && f.Output != outputJSON {
		return microerror.Maskf(invalidFlagError, "--output must be %s or %s", outputText, outputJSON)
	}
	if f.Team != "" && !strings.HasPrefix(f.Team, "team-") {
		f.Team = "team-" + f.Team
	}
	return nil
}
