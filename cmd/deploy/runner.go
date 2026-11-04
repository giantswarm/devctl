package deploy

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/appstatus"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

type runner struct {
	Flag   *flag
	Logger *logrus.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	err := r.Flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}

	token, err := authstore.ResolveGitHub(ctx)
	if err != nil {
		return err
	}
	token.WarnOnce(r.Stderr)
	notFoundHint := authstore.GitHubNotFoundHint(token)

	githubClient, err := githubclient.New(githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token.Value,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	// Parse repository name
	owner, repo, err := parseGitOpsRepo(r.Flag.GitOpsRepo)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create temporary directory for GitOps files
	tempDir, err := os.MkdirTemp("", "gitops-*")
	if err != nil {
		return microerror.Mask(err)
	}
	r.Logger.Infof("Created temporary directory: %s", tempDir)
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Clone repository
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}
	err = githubClient.CloneRepository(ctx, owner, repo, tempDir)
	if err != nil {
		return githubclient.ExplainNotFound(microerror.Mask(err), notFoundHint)
	}
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}

	// Create new branch
	newBranch := fmt.Sprintf("%s-%s-%s", r.Flag.WorkloadCluster, r.Flag.AppName, r.Flag.AppVersion)
	newBranch = strings.ReplaceAll(newBranch, ":", "-")
	err = githubClient.CreateBranch(ctx, newBranch)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.addApp(ctx, tempDir)
	if err != nil {
		return microerror.Mask(err)
	}

	// Commit and push changes
	commitMsg := fmt.Sprintf("Add the app %s version %s on %s cluster GitOps repository", r.Flag.AppName, r.Flag.AppVersion, r.Flag.WorkloadCluster)
	err = githubClient.CommitAndPush(ctx, owner, repo, newBranch, commitMsg)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create pull request
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}
	pr, err := githubClient.CreatePullRequest(ctx, owner, repo, newBranch, commitMsg)
	if err != nil {
		return githubclient.ExplainNotFound(microerror.Mask(err), notFoundHint)
	}

	r.Logger.Infof("PR created: %s. Please approve to continue.", pr.GetHTMLURL())
	// Wait for PR merge
	err = githubClient.WaitForPRMerge(ctx, owner, repo, pr.GetNumber(), time.Duration(r.Flag.Timeout)*time.Second)
	if err != nil {
		return microerror.Mask(err)
	}

	// Check app status
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}
	appStatusClient, err := appstatus.New(appstatus.Config{
		Logger: r.Logger,
		Stderr: r.Stderr,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	orgNamespace := "org-" + r.Flag.Organization
	appName := r.Flag.WorkloadCluster + "-" + r.Flag.AppName
	err = appStatusClient.WaitForAppDeployment(ctx, appName, orgNamespace, r.Flag.ManagementCluster, time.Duration(r.Flag.Timeout)*time.Second)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func parseGitOpsRepo(repo string) (string, string, error) {
	s := strings.Split(repo, "/")
	if len(s) != 2 {
		return "", "", microerror.Maskf(invalidArgError, "invalid GitOps repository format: %q", repo)
	}
	return s[0], s[1], nil
}
