package githubclient

import (
	"context"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// PullRequestMerge is what release wait reads of a pull request.
type PullRequestMerge struct {
	Number int
	State  string
	Merged bool
	// MergeCommitSHA is the commit on the base branch; empty until merged.
	MergeCommitSHA string
	MergedAt       time.Time
}

// GetPullRequestMerge returns the merge state of a pull request.
func (c *Client) GetPullRequestMerge(ctx context.Context, owner, repo string, number int) (PullRequestMerge, error) {
	pr, err := c.PullRequest(ctx, owner, repo, number)
	if err != nil {
		return PullRequestMerge{}, microerror.Mask(err)
	}
	m := PullRequestMerge{Number: pr.GetNumber(), State: pr.GetState(), Merged: pr.GetMerged()}
	if m.Merged {
		m.MergeCommitSHA = pr.GetMergeCommitSHA()
		m.MergedAt = pr.GetMergedAt().Time
	}
	return m, nil
}

// GetTagSHA returns the commit a tag points at, through an annotated tag
// object when the tag is one. IsNotFound when the tag does not exist.
func (c *Client) GetTagSHA(ctx context.Context, owner, repo, tag string) (string, error) {
	ref, _, err := c.ghClient.Git.GetRef(ctx, owner, repo, "tags/"+tag)
	if isGithub404(err) {
		return "", microerror.Maskf(notFoundError, "tag %s in %s/%s", tag, owner, repo)
	}
	if err != nil {
		return "", microerror.Mask(err)
	}
	obj := ref.GetObject()
	if obj.GetType() != "tag" {
		return obj.GetSHA(), nil
	}
	annotated, _, err := c.ghClient.Git.GetTag(ctx, owner, repo, obj.GetSHA())
	if err != nil {
		return "", microerror.Mask(err)
	}
	return annotated.GetObject().GetSHA(), nil
}

// tagPages bounds the tag listing of FindTagForCommit: the tag of a fresh
// merge is among the newest.
const tagPages = 3

// FindTagForCommit returns the newest tag pointing at sha, or "" when the
// newest tags do not include one.
func (c *Client) FindTagForCommit(ctx context.Context, owner, repo, sha string) (string, error) {
	opts := &github.ListOptions{PerPage: 100}
	for page := 0; page < tagPages; page++ {
		tags, resp, err := c.ghClient.Repositories.ListTags(ctx, owner, repo, opts)
		if err != nil {
			return "", microerror.Mask(err)
		}
		for _, t := range tags {
			if t.GetCommit().GetSHA() == sha {
				return t.GetName(), nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return "", nil
}

// Release is a GitHub release as release wait reads it.
type Release struct {
	URL string
	// Published: not a draft.
	Published bool
	Assets    []ReleaseAsset
}

// ReleaseAsset is one uploaded asset with the digest GitHub reports for it.
type ReleaseAsset struct {
	Name   string
	URL    string
	Digest string
}

// GetReleaseByTag returns the release of a tag; IsNotFound when there is none.
func (c *Client) GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error) {
	rel, _, err := c.ghClient.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if isGithub404(err) {
		return Release{}, microerror.Maskf(notFoundError, "release %s in %s/%s", tag, owner, repo)
	}
	if err != nil {
		return Release{}, microerror.Mask(err)
	}
	r := Release{URL: rel.GetHTMLURL(), Published: !rel.GetDraft(), Assets: []ReleaseAsset{}}
	for _, a := range rel.Assets {
		r.Assets = append(r.Assets, ReleaseAsset{Name: a.GetName(), URL: a.GetBrowserDownloadURL(), Digest: a.GetDigest()})
	}
	return r, nil
}

// WorkflowRun is one GitHub Actions run.
type WorkflowRun struct {
	Name       string
	Path       string // the workflow file, .github/workflows/<file>
	ID         int64
	Event      string
	HeadBranch string
	Status     string
	Conclusion string
	URL        string
	CreatedAt  time.Time
}

// ListWorkflowRunsForSHA returns the Actions runs on a commit, newest first,
// as [WorkflowRun] values.
func (c *Client) ListWorkflowRunsForSHA(ctx context.Context, owner, repo, sha string) ([]WorkflowRun, error) {
	runs, err := c.WorkflowRunsForSHA(ctx, owner, repo, sha)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	out := make([]WorkflowRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, WorkflowRun{
			Name:       r.GetName(),
			Path:       r.GetPath(),
			ID:         r.GetID(),
			Event:      r.GetEvent(),
			HeadBranch: r.GetHeadBranch(),
			Status:     r.GetStatus(),
			Conclusion: r.GetConclusion(),
			URL:        r.GetHTMLURL(),
			CreatedAt:  r.GetCreatedAt().Time,
		})
	}
	return out, nil
}

// ListDirectory returns the names of the entries of a directory at ref
// (files and directories alike). IsNotFound when the path does not exist.
func (c *Client) ListDirectory(ctx context.Context, owner, repo, path, ref string) ([]string, error) {
	_, dir, _, err := c.ghClient.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
	if isGithub404(err) {
		return nil, microerror.Maskf(notFoundError, "directory %q at %s in %s/%s", path, ref, owner, repo)
	}
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if dir == nil {
		return nil, microerror.Maskf(executionError, "expected a directory but content under path %#q is a file", path)
	}
	names := make([]string, 0, len(dir))
	for _, entry := range dir {
		names = append(names, entry.GetName())
	}
	return names, nil
}

// IsPrivateRepository says whether owner/repo is private.
func (c *Client) IsPrivateRepository(ctx context.Context, owner, repo string) (bool, error) {
	r, _, err := c.ghClient.Repositories.Get(ctx, owner, repo)
	if isGithub404(err) {
		return false, microerror.Maskf(notFoundError, "repository %s/%s", owner, repo)
	}
	if err != nil {
		return false, microerror.Mask(err)
	}
	return r.GetPrivate(), nil
}
