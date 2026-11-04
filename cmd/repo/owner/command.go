// Package owner exposes the declared owning team without listing repository
// access grants, which the agent GitHub App cannot read.
package owner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const longDesc = `Print the GitHub team slug whose file in giantswarm/github declares a
repository. Reads repositories/*.yaml at main with the devctl GitHub App
login (contents: read), without calling GET /repos/{owner}/{repo}/teams.
This is the supported ownership lookup before a merge or review request;
it reports declared ownership, not every team with access on GitHub.

Only giantswarm repositories are described by this inventory. An undeclared
repository or an unreadable team file is an error, never a guessed owner.
--output json includes the repository, team and source team-file URL.

Examples:
  devctl repo owner devctl
  devctl repo owner giantswarm/devctl --output json`

type Config struct {
	Logger *logrus.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	var output string
	c := &cobra.Command{
		Use:   "owner [OWNER/]REPOSITORY",
		Short: "Print the declared owning team from the team-file inventory",
		Long:  longDesc,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				return microerror.Maskf(invalidConfigError, "--output must be text or json, got %#q", output)
			}
			return microerror.Mask(run(cmd.Context(), config, args[0], output))
		},
	}
	c.Flags().StringVarP(&output, "output", "o", "text", "Output format: text or json.")
	return c, nil
}

type result struct {
	Repository string `json:"repository"`
	Team       string `json:"team"`
	Source     string `json:"source"`
}

func run(ctx context.Context, config Config, arg, output string) error {
	if !strings.Contains(arg, "/") {
		arg = reposetup.DefaultOwner + "/" + arg
	}
	owner, repo, err := agentcli.ParseRepository(arg)
	if err != nil {
		return microerror.Mask(err)
	}
	if !strings.EqualFold(owner, reposetup.DefaultOwner) {
		return microerror.Maskf(invalidConfigError, "the team-file inventory describes only %s repositories, got %#q", reposetup.DefaultOwner, owner)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	token, err := authstore.ResolveGitHub(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	token.WarnOnce(config.Stderr)
	client, err := githubclient.New(githubclient.Config{Logger: config.Logger, AccessToken: token.Value})
	if err != nil {
		return microerror.Mask(err)
	}
	remote := reposetup.Remote{GitHub: client.GetUnderlyingClient(ctx)}
	tf, err := remote.FindEntry(ctx, repo, nil)
	if err != nil {
		return microerror.Mask(err)
	}

	if output == "json" {
		return microerror.Mask(json.NewEncoder(config.Stdout).Encode(result{
			Repository: reposetup.DefaultOwner + "/" + repo,
			Team:       tf.Team,
			Source:     fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", reposetup.SchemaRepositoryOwner, reposetup.SchemaRepository, reposetup.SchemaRef, tf.Path),
		}))
	}
	_, err = fmt.Fprintln(config.Stdout, tf.Team)
	return microerror.Mask(err)
}
