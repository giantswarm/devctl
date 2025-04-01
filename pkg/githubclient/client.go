package githubclient

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
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

func (c *Client) CloneRepository(ctx context.Context, owner, repo string) error {
	_, err := git.PlainClone(c.workDir, false, &git.CloneOptions{
		URL:      fmt.Sprintf("https://%s@github.com/%s/%s", c.accessToken, owner, repo),
		Progress: os.Stdout,
	})

	return microerror.Mask(err)
}

func (c *Client) CreateBranch(ctx context.Context, newBranch string) error {
	repo, err := git.PlainOpen(c.workDir)
	if err != nil {
		return microerror.Mask(err)
	}

	// Get the current HEAD
	head, err := repo.Head()
	if err != nil {
		return microerror.Mask(err)
	}

	// Create and checkout new branch
	worktree, err := repo.Worktree()
	if err != nil {
		return microerror.Mask(err)
	}

	c.logger.Infof("creating new branch %s", newBranch)
	err = worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(newBranch),
		Create: true,
		Hash:   head.Hash(),
	})
	if err != nil {
		return microerror.Mask(err)
	}

	c.logger.Infof("created branch %s", newBranch)
	return nil
}

func (c *Client) CommitAndPush(ctx context.Context, branch, message string) error {
	repo, err := git.PlainOpen(c.workDir)
	if err != nil {
		return microerror.Mask(err)
	}

	// Get the worktree
	worktree, err := repo.Worktree()
	if err != nil {
		return microerror.Mask(err)
	}

	// Stage all changes
	_, err = worktree.Add(".")
	if err != nil {
		return microerror.Mask(err)
	}

	// Commit changes
	_, err = worktree.Commit(message, &git.CommitOptions{})
	if err != nil {
		return microerror.Mask(err)
	}

	// Push changes
	err = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))},
	})
	if err != nil {
		return microerror.Mask(err)
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
