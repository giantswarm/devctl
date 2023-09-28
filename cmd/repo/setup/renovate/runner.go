package renovate

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/githubclient"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	s := strings.Split(args[0], "/")
	if len(s) != 2 {
		return microerror.Maskf(invalidArgError, "expected owner/repo, got %s", args[0])
	}

	owner := s[0]
	repo := s[1]

	var tokenEnv string
	flag := cmd.Flag("github-token-envvar")
	if flag != nil {
		tokenEnv = flag.Value.String()
	}

	var token string
	var found bool
	if token, found = os.LookupEnv(tokenEnv); !found {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q was not found", r.flag.GithubTokenEnvVar)
	}

	c := githubclient.Config{
		Logger:      r.logger,
		AccessToken: token,
	}

	client, err := githubclient.New(c)
	if err != nil {
		return microerror.Mask(err)
	}

	repository, err := client.GetRepository(ctx, owner, repo)
	if err != nil {
		return microerror.Mask(err)
	}

	if r.flag.Remove {
		r.logger.Printf("Removing %s/%s from repositories accessible by Renovate...", owner, *repository.Name)
		err = client.RemoveRepoFromRenovatePermissions(ctx, owner, repository)
	} else {
		r.logger.Printf("Adding %s/%s to repositories accessible by Renovate...", owner, *repository.Name)
		err = client.AddRepoToRenovatePermissions(ctx, owner, repository)
	}
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
