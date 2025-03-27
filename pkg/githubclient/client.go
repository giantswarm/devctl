package githubclient

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v70/github"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

type Config struct {
	Logger      *logrus.Logger
	AccessToken string
}

type Client struct {
	logger      *logrus.Logger
	accessToken string
	workDir     string
}

func New(config Config) (*Client, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.AccessToken == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.AccessToken must not be empty", config)
	}

	c := &Client{
		logger:      config.Logger,
		accessToken: config.AccessToken,
	}

	return c, nil
}

func (c *Client) CloneRepository(ctx context.Context, owner, repo, tempDir string) error {
	c.workDir = tempDir
	url := fmt.Sprintf("git@github.com:%s/%s.git", owner, repo)
	cmd := exec.Command("git", "clone", "-b", "main", url, c.workDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (c *Client) CreateBranch(ctx context.Context, owner, repo, newBranch string) error {
	client := c.getUnderlyingClient(ctx)
	ref, _, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/main")
	if err != nil {
		return microerror.Mask(err)
	}

	newRef := &github.Reference{
		Ref: github.Ptr("refs/heads/" + newBranch),
		Object: &github.GitObject{
			SHA: ref.Object.SHA,
		},
	}

	_, _, err = client.Git.CreateRef(ctx, owner, repo, newRef)
	if err != nil {
		return microerror.Mask(err)
	}
	c.logger.Infof("created branch %s", newBranch)

	return nil
}

func (c *Client) CommitAndPush(ctx context.Context, owner, repo, branch, message string) error {
	// Stage all changes
	cmd := exec.Command("git", "add", ".", "-A")
	cmd.Dir = c.workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to add stage content %v\n%s", err, output)
	}
	c.logger.Infof("work directory: %s", c.workDir)

	// Commit changes
	c.logger.Infof("committing changes with message: %s", message)
	cmd = exec.Command("git", "commit", "-am", message)
	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to commit changes: %v\n%s", err, output)
	}

	// Push changes
	cmd = exec.Command("git", "push", "origin", branch)
	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to push changes: %v\n%s", err, output)
	}

	c.logger.Infof("pushed changes to branch %s", branch)
	return nil
}

func (c *Client) CreatePullRequest(ctx context.Context, owner, repo, head, title string) (*github.PullRequest, error) {
	client := c.getUnderlyingClient(ctx)
	newPR := &github.NewPullRequest{
		Title: github.Ptr(title),
		Head:  github.Ptr(head),
		Base:  github.Ptr("main"),
		Body:  github.Ptr(title),
	}

	pr, _, err := client.PullRequests.Create(ctx, owner, repo, newPR)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return pr, nil
}

func (c *Client) WaitForPRMerge(ctx context.Context, owner, repo string, prNumber int, timeout time.Duration) error {
	client := c.getUnderlyingClient(ctx)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return microerror.Maskf(prMergeTimeoutError, "PR #%d was not merged within %v", prNumber, timeout)
		}

		pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
		if err != nil {
			return microerror.Mask(err)
		}

		if pr.Merged != nil && *pr.Merged {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			continue
		}
	}
}

func (c *Client) getUnderlyingClient(ctx context.Context) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{
			AccessToken: c.accessToken,
		},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	return client
}
