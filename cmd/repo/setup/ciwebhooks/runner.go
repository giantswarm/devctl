package ciwebhooks

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v68/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/githubclient"
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

	token, found := os.LookupEnv(r.flag.GithubTokenEnvVar)
	if !found {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q was not found", r.flag.GithubTokenEnvVar)
	}

	c := githubclient.Config{
		DryRun:      r.flag.DryRun,
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

	hook := &github.Hook{
		Name:   github.String("web"),
		Active: github.Bool(true),
		Events: []string{"issue_comment", "pull_request", "check_run"},
		Config: &github.HookConfig{
			URL:         &r.flag.WebhookURL,
			ContentType: github.String("json"),
			InsecureSSL: github.String("0"),
			Secret:      github.String(r.flag.WebhookSharedSecret),
		},
	}

	err = client.SetRepositoryWebhooks(ctx, repository, hook)
	if err != nil {
		return microerror.Mask(err)
	}

	r.logger.Info("completed repository setup")

	return nil
}
