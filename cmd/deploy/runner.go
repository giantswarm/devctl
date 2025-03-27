package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/githubclient"
)

type runner struct {
	Flag   *flag
	Logger micrologger.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.Flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	// Clone GitOps repository
	repoDir, err := r.cloneGitOpsRepo(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	defer os.RemoveAll(repoDir)

	// Execute kubectl gs gitops add app
	err = r.executeGitOpsAddApp(ctx, repoDir)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create branch and commit changes
	err = r.createBranchAndCommit(ctx, repoDir)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create pull request
	err = r.createPullRequest(ctx, repoDir)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) cloneGitOpsRepo(ctx context.Context) (string, error) {
	// Split GitOps repo into owner and name
	s := strings.Split(r.Flag.GitOpsRepo, "/")
	if len(s) != 2 {
		return "", microerror.Maskf(invalidArgError, "expected owner/repo, got %s", r.Flag.GitOpsRepo)
	}
	owner := s[0]
	repo := s[1]

	// Get GitHub token
	var token string
	var found bool
	if token, found = os.LookupEnv(r.Flag.GithubTokenEnv); !found {
		return "", microerror.Maskf(envVarNotFoundError, "environment variable %#q was not found", r.Flag.GithubTokenEnv)
	}

	// Create temporary directory for GitOps files
	tmpDir, err := os.MkdirTemp("", "gitops-*")
	if err != nil {
		return "", microerror.Mask(err)
	}

	// Initialize GitHub client
	githubConfig := githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token,
	}

	githubClient, err := githubclient.New(githubConfig)
	if err != nil {
		return "", microerror.Mask(err)
	}

	// Clone GitOps repository
	r.Logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Cloning %s/%s to %s", owner, repo, tmpDir))
	err = githubClient.CloneRepository(ctx, owner, repo, r.Flag.GitOpsBranch, tmpDir)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return tmpDir, nil
}

func (r *runner) executeGitOpsAddApp(ctx context.Context, repoDir string) error {
	// Run kubectl gs gitops add app command
	kubectlCmd := exec.Command("kubectl", "gs", "gitops", "add", "app",
		"--local-path", repoDir,
		"--management-cluster", r.Flag.ManagementCluster,
		"--organization", r.Flag.Organization,
		"--workload-cluster", r.Flag.WorkloadCluster,
		"--app", r.Flag.AppName,
		"--catalog", r.Flag.AppCatalog,
		"--version", r.Flag.AppVersion,
		"--target-namespace", r.Flag.AppNamespace,
		"--name", r.Flag.AppName,
	)

	kubectlCmd.Stdout = r.Stdout
	kubectlCmd.Stderr = r.Stderr

	r.Logger.LogCtx(ctx, "level", "info", "message", "Running kubectl gs gitops add app command")
	err := kubectlCmd.Run()
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) createBranchAndCommit(ctx context.Context, repoDir string) error {
	// Split GitOps repo into owner and name
	s := strings.Split(r.Flag.GitOpsRepo, "/")
	if len(s) != 2 {
		return microerror.Maskf(invalidArgError, "expected owner/repo, got %s", r.Flag.GitOpsRepo)
	}
	owner := s[0]
	repo := s[1]

	// Create new branch
	newBranch := fmt.Sprintf("deploy/%s/%s", r.Flag.AppName, r.Flag.AppVersion)

	// Get GitHub token
	var token string
	var found bool
	if token, found = os.LookupEnv(r.Flag.GithubTokenEnv); !found {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q was not found", r.Flag.GithubTokenEnv)
	}

	// Initialize GitHub client
	githubConfig := githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token,
	}

	githubClient, err := githubclient.New(githubConfig)
	if err != nil {
		return microerror.Mask(err)
	}

	err = githubClient.CreateBranch(ctx, owner, repo, r.Flag.GitOpsBranch, newBranch)
	if err != nil {
		return microerror.Mask(err)
	}

	// Commit and push changes
	commitMsg := fmt.Sprintf("Deploy %s version %s", r.Flag.AppName, r.Flag.AppVersion)
	err = githubClient.CommitAndPush(ctx, owner, repo, newBranch, commitMsg)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) createPullRequest(ctx context.Context, repoDir string) error {
	// Split GitOps repo into owner and name
	s := strings.Split(r.Flag.GitOpsRepo, "/")
	if len(s) != 2 {
		return microerror.Maskf(invalidArgError, "expected owner/repo, got %s", r.Flag.GitOpsRepo)
	}
	owner := s[0]
	repo := s[1]

	newBranch := fmt.Sprintf("deploy/%s/%s", r.Flag.AppName, r.Flag.AppVersion)
	commitMsg := fmt.Sprintf("Deploy %s version %s", r.Flag.AppName, r.Flag.AppVersion)

	// Get GitHub token
	var token string
	var found bool
	if token, found = os.LookupEnv(r.Flag.GithubTokenEnv); !found {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q was not found", r.Flag.GithubTokenEnv)
	}

	// Initialize GitHub client
	githubConfig := githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token,
	}

	githubClient, err := githubclient.New(githubConfig)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create pull request
	r.Logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Creating pull request for %s version %s", r.Flag.AppName, r.Flag.AppVersion))
	pr, err := githubClient.CreatePullRequest(ctx, owner, repo, newBranch, r.Flag.GitOpsBranch, commitMsg)
	if err != nil {
		return microerror.Mask(err)
	}

	if r.Flag.DryRun {
		r.Logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Dry run: Would create PR #%d", pr.GetNumber()))
		return nil
	}

	// Wait for PR to be merged
	r.Logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Waiting for PR #%d to be merged...", pr.GetNumber()))
	err = githubClient.WaitForPRMerge(ctx, owner, repo, pr.GetNumber(), time.Duration(r.Flag.Timeout)*time.Second)
	if err != nil {
		return microerror.Mask(err)
	}

	r.Logger.LogCtx(ctx, "level", "info", "message", fmt.Sprintf("Successfully deployed %s version %s", r.Flag.AppName, r.Flag.AppVersion))
	return nil
}
