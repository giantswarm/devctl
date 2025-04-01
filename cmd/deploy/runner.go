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
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/appstatus"
	"github.com/giantswarm/devctl/v7/pkg/githubclient"
)

type runner struct {
	Flag   *flag
	Logger *logrus.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	var addAppCmd *exec.Cmd

	err := r.Flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	// Get GitHub token from environment variables
	token := getGitHubToken()
	if token == "" {
		return microerror.Maskf(envVarNotFoundError, "GitHub token not found in environment variables. Please set GITHUB_TOKEN or OPSCTL_GITHUB_TOKEN")
	}

	// Create GitHub client
	githubClient, err := githubclient.New(githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	// Clone GitOps repository
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
	defer os.RemoveAll(tempDir)

	// Clone repository
	err = githubClient.CloneRepository(ctx, owner, repo, tempDir)
	if err != nil {
		return microerror.Mask(err)
	}

	// Create new branch
	newBranch := fmt.Sprintf("deploy-%s-%s-%s-%s", r.Flag.WorkloadCluster, r.Flag.AppName, r.Flag.AppVersion, time.Now().Format("15:04"))
	newBranch = strings.ReplaceAll(newBranch, ":", "-")
	err = githubClient.CreateBranch(ctx, newBranch)
	if err != nil {
		return microerror.Mask(err)
	}

	// Execute kubectl gs gitops add app
	addAppArgs := []string{
		"gs", "gitops", "add", "app",
		"--app", r.Flag.AppName,
		"--version", r.Flag.AppVersion,
		"--catalog", r.Flag.AppCatalog,
		"--target-namespace", r.Flag.AppNamespace,
		"--management-cluster", r.Flag.ManagementCluster,
		"--workload-cluster", r.Flag.WorkloadCluster,
		"--organization", r.Flag.Organization,
	}
	addAppCmd = exec.Command("kubectl", addAppArgs...)
	addAppCmd.Dir = tempDir
	addAppCmd.Stdout = r.Stdout
	addAppCmd.Stderr = r.Stderr
	err = addAppCmd.Run()
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
	pr, err := githubClient.CreatePullRequest(ctx, owner, repo, newBranch, commitMsg)
	if err != nil {
		return microerror.Mask(err)
	}

	r.Logger.Infof("PR created: %s. Please approve to continue.", pr.GetHTMLURL())
	// Wait for PR merge
	err = githubClient.WaitForPRMerge(ctx, owner, repo, pr.GetNumber(), time.Duration(r.Flag.Timeout)*time.Second)
	if err != nil {
		return microerror.Mask(err)
	}

	// Check app status
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

func getGitHubToken() string {
	// Try GITHUB_TOKEN first
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}

	// Try OPSCTL_GITHUB_TOKEN as fallback
	if token := os.Getenv("OPSCTL_GITHUB_TOKEN"); token != "" {
		return token
	}

	return ""
}
