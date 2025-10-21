package setup

import (
	"context"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v76/github"
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

	var ChecksFilterRegexp *regexp.Regexp
	if r.flag.ChecksFilter != "" {
		ChecksFilterRegexp, err = regexp.Compile(r.flag.ChecksFilter)
		if err != nil {
			return microerror.Mask(err)
		}
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

	err = client.SetRepositoryDefaultBranch(ctx, repository, r.flag.DefaultBranch)
	if err != nil {
		return microerror.Mask(err)
	}

	// Branch protection
	if r.flag.DisableBranchProtection {
		err = client.RemoveRepositoryBranchProtection(ctx, repository)
		if err != nil {
			return microerror.Mask(err)
		}
	} else {
		err = client.SetRepositoryBranchProtection(ctx, repository, r.flag.Checks, ChecksFilterRegexp)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// Renovate-related setup
	if r.flag.SetupRenovate {
		rulesetNeedsUpdate := false

		// Add repository to Renovate permissions
		r.logger.Infof("Adding %s/%s to repositories accessible by Renovate...", owner, repo)
		err = client.AddRepoToRenovatePermissions(ctx, owner, repository)
		if err != nil {
			return microerror.Mask(err)
		}

		// Find ruleset in repository
		ruleset, err := client.ReadRenovateRuleset(ctx, owner, repo)
		if ruleset != nil {
			r.logger.Printf("Ruleset %s (ID %d) already exists in repository %s/%s", ruleset.Name, ruleset.GetID(), owner, repo)

			// check if up-to-date
			if client.IsRulesetUpToDate(*ruleset) {
				r.logger.Infof("Ruleset %s (ID %d) is up-to-date in repository %s/%s", ruleset.Name, ruleset.GetID(), owner, repo)
			} else {
				r.logger.Infof("Ruleset %s (ID %d) is not up-to-date in repository %s/%s", ruleset.Name, ruleset.GetID(), owner, repo)
				rulesetNeedsUpdate = true
				r.logger.Infof("Deleting ruleset %s (ID %d) in repository %s/%s", ruleset.Name, ruleset.GetID(), owner, repo)
				err = client.DeleteRuleset(ctx, owner, repo, ruleset.GetID())
				if err != nil {
					return microerror.Mask(err)
				}
			}
		} else {
			// handle not found error
			if githubclient.IsRulesetNotFound(err) {
				r.logger.Infof("Ruleset for renovate not found in repository %s/%s, creating it...", owner, repo)
				rulesetNeedsUpdate = true
			} else {
				return microerror.Mask(err)
			}
		}

		if rulesetNeedsUpdate {
			r.logger.Infof("Creating ruleset for renovate in repository %s/%s", owner, repo)
			_, err = client.CreateRenovateRuleset(ctx, owner, repo)
			if err != nil {
				return microerror.Mask(err)
			}
		}

	} else {
		r.logger.Printf("Removing %s/%s from repositories accessible by Renovate...", owner, *repository.Name)
		err = client.RemoveRepoFromRenovatePermissions(ctx, owner, repository)
		if err != nil {
			// Not a critical error, we can continue
			r.logger.Errorf("error removing %s/%s from repositories accessible by Renovate: %v", owner, *repository.Name, err)
		}
	}

	r.logger.Info("completed repository setup")

	return nil
}
