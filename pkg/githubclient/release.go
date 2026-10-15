package githubclient

import (
	"context"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
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

// tagPages bounds the tag listing of FindTagForCommit's first pass: the tag
// of a fresh merge into the default branch is among the newest.
const tagPages = 3

// FindTagForCommit returns the newest tag pointing at sha, or "" when no tag
// does. The tag listing is ordered by version, newest first, so the tag of a
// fresh merge into the default branch is in its first pages; a tag of an
// older line (a release branch) is read from [Client.CommitTags].
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
			return "", nil
		}
		opts.Page = resp.NextPage
	}
	tags, err := c.CommitTags(ctx, owner, repo)
	if err != nil {
		return "", microerror.Mask(err)
	}
	return NewestTag(tags[sha]), nil
}

// CommitTags maps each commit a lightweight tag points at to the names of
// those tags, every tag of the repository read in one request. Auto-release
// pushes lightweight tags; an annotated tag points at its tag object and is
// not in the map.
func (c *Client) CommitTags(ctx context.Context, owner, repo string) (map[string][]string, error) {
	refs, _, err := c.ghClient.Git.ListMatchingRefs(ctx, owner, repo, "tags/")
	if isGithub404(err) {
		return map[string][]string{}, nil
	}
	if err != nil {
		return nil, microerror.Mask(err)
	}
	out := map[string][]string{}
	for _, ref := range refs {
		if ref.GetObject().GetType() != "commit" {
			continue
		}
		sha := ref.GetObject().GetSHA()
		out[sha] = append(out[sha], strings.TrimPrefix(ref.GetRef(), "refs/tags/"))
	}
	return out, nil
}

// NewestTag is the highest version among tags, a name that is no version
// ranking below every version; "" for none.
func NewestTag(tags []string) string {
	newest := ""
	var newestVersion *semver.Version
	for _, tag := range tags {
		v, err := semver.NewVersion(tag)
		switch {
		case newest == "":
			newest, newestVersion = tag, v
		case err != nil:
		case newestVersion == nil || v.GreaterThan(newestVersion):
			newest, newestVersion = tag, v
		}
	}
	return newest
}

// Release is a GitHub release as release wait and release promote read it.
type Release struct {
	// Tag is the release's tag name.
	Tag string
	URL string
	// Published: not a draft.
	Published bool
	// Prerelease: marked as a pre-release.
	Prerelease bool
	Assets     []ReleaseAsset
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
	r := Release{Tag: rel.GetTagName(), URL: rel.GetHTMLURL(), Published: !rel.GetDraft(), Prerelease: rel.GetPrerelease(), Assets: []ReleaseAsset{}}
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

// WorkflowJob is one job of a GitHub Actions run.
type WorkflowJob struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
	URL        string
}

// ListWorkflowRunJobs returns the jobs of the latest attempt of an Actions
// run as [WorkflowJob] values.
func (c *Client) ListWorkflowRunJobs(ctx context.Context, owner, repo string, runID int64) ([]WorkflowJob, error) {
	opts := &github.ListWorkflowJobsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	var out []WorkflowJob
	for page := 0; page < maxPages; page++ {
		jobs, resp, err := c.ghClient.Actions.ListWorkflowJobs(ctx, owner, repo, runID, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		for _, j := range jobs.Jobs {
			out = append(out, WorkflowJob{ID: j.GetID(), Name: j.GetName(), Status: j.GetStatus(), Conclusion: j.GetConclusion(), URL: j.GetHTMLURL()})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// CommitStatus is one commit status of a commit.
type CommitStatus struct {
	Context   string
	State     string // pending, success, failure or error
	TargetURL string
}

// ListCommitStatuses returns every commit status of sha, newest first, as
// [CommitStatus] values: a context's earlier statuses too, which the
// combined status drops, so the statuses of every pipeline that built the
// commit are there when two tags point at it. IsNotFound when the
// repository has no such commit.
func (c *Client) ListCommitStatuses(ctx context.Context, owner, repo, sha string) ([]CommitStatus, error) {
	opts := &github.ListOptions{PerPage: 100}
	var out []CommitStatus
	for page := 0; page < maxPages; page++ {
		statuses, resp, err := c.ghClient.Repositories.ListStatuses(ctx, owner, repo, sha, opts)
		if isGithub404(err) {
			return nil, microerror.Maskf(notFoundError, "commit %s in %s/%s", sha, owner, repo)
		}
		if err != nil {
			return nil, microerror.Mask(err)
		}
		for _, s := range statuses {
			out = append(out, CommitStatus{Context: s.GetContext(), State: s.GetState(), TargetURL: s.GetTargetURL()})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
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
