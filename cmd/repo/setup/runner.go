package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v44/github"
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

	token, found := os.LookupEnv(r.flag.GithubTokenEnvVar)
	if !found {
		return microerror.Maskf(envVarNotFoundError, "environement variable %#q was not found", r.flag.GithubTokenEnvVar)
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

	repositorySettings := &github.Repository{
		HasWiki:     &r.flag.EnableWiki,
		HasIssues:   &r.flag.EnableIssues,
		HasProjects: &r.flag.EnableProjects,
		Archived:    &r.flag.Archived,

		AllowMergeCommit: &r.flag.AllowMergeCommit,
		AllowSquashMerge: &r.flag.AllowSquashMerge,
		AllowRebaseMerge: &r.flag.AllowRebaseMerge,

		AllowUpdateBranch:   &r.flag.AllowUpdateBranch,
		AllowAutoMerge:      &r.flag.AllowAutoMerge,
		DeleteBranchOnMerge: &r.flag.DeleteBranchOnMerge,
	}

	repository, err = client.SetRepositorySettings(ctx, repository, repositorySettings)
	if err != nil {
		return microerror.Mask(err)
	}

	err = client.SetRepositoryPermissions(ctx, repository, r.flag.Permissions)
	if err != nil {
		return microerror.Mask(err)
	}

	err = client.SetRepositoryBranchProtection(ctx, repository, r.flag.Checks)
	if err != nil {
		return microerror.Mask(err)
	}

	fmt.Printf("completed repository setup for %v\n", *repository.Name)

	return nil
}
