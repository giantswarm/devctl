package githubclient

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// CombinedStatus is the combined commit status of a ref: the state GitHub
// derives from the latest status of every context, and how many contexts
// reported. With no context GitHub still answers pending.
type CombinedStatus struct {
	State      string
	TotalCount int
	// Statuses is the latest status of every context, the first
	// [combinedStatusPage] of them.
	Statuses []CommitStatus
}

// combinedStatusPage bounds the contexts GetCombinedStatus reads: one page,
// the most GitHub serves.
const combinedStatusPage = 100

// GetCombinedStatus returns the combined commit status of ref, a branch, tag
// or SHA. IsNotFound when the ref does not exist.
func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, ref string) (CombinedStatus, error) {
	combined, _, err := c.ghClient.Repositories.GetCombinedStatus(ctx, owner, repo, ref, &github.ListOptions{PerPage: combinedStatusPage})
	if isGithub404(err) {
		return CombinedStatus{}, microerror.Maskf(notFoundError, "ref %s in %s/%s", ref, owner, repo)
	}
	if err != nil {
		return CombinedStatus{}, microerror.Mask(err)
	}
	status := CombinedStatus{State: combined.GetState(), TotalCount: combined.GetTotalCount()}
	for _, s := range combined.Statuses {
		status.Statuses = append(status.Statuses, CommitStatus{Context: s.GetContext(), State: s.GetState(), TargetURL: s.GetTargetURL()})
	}
	return status, nil
}

// releasePages bounds ListReleases: 1000 releases.
const releasePages = 10

// ListReleases returns the releases of a repository, drafts included, in the
// order GitHub lists them (newest first).
func (c *Client) ListReleases(ctx context.Context, owner, repo string) ([]Release, error) {
	opts := &github.ListOptions{PerPage: 100}
	var out []Release
	for range releasePages {
		releases, resp, err := c.ghClient.Repositories.ListReleases(ctx, owner, repo, opts)
		if isGithub404(err) {
			return nil, microerror.Maskf(notFoundError, "repository %s/%s", owner, repo)
		}
		if err != nil {
			return nil, microerror.Mask(err)
		}
		for _, rel := range releases {
			out = append(out, Release{
				Tag:        rel.GetTagName(),
				URL:        rel.GetHTMLURL(),
				Published:  !rel.GetDraft(),
				Prerelease: rel.GetPrerelease(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// DefaultBranch returns the default branch of a repository. IsNotFound when
// the repository does not exist or the token cannot read it.
func (c *Client) DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, _, err := c.ghClient.Repositories.Get(ctx, owner, repo)
	if isGithub404(err) {
		return "", microerror.Maskf(notFoundError, "repository %s/%s", owner, repo)
	}
	if err != nil {
		return "", microerror.Mask(err)
	}
	return r.GetDefaultBranch(), nil
}

// ReadFile returns the content of the file at path at ref, and false when
// there is no such file.
func (c *Client) ReadFile(ctx context.Context, owner, repo, path, ref string) ([]byte, bool, error) {
	file, dir, _, err := c.ghClient.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
	if isGithub404(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, microerror.Mask(err)
	}
	if file == nil {
		return nil, false, microerror.Maskf(executionError, "expected a file but content under path %#q is a directory (%d entries)", path, len(dir))
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, false, microerror.Mask(err)
	}
	return []byte(content), true, nil
}

// Reachable says whether ref, a tag or SHA, is reachable from branch: the
// comparison of ref (base) with branch (head) finds branch ahead of ref or
// identical to it. behind or diverged means ref is not in branch's history.
// IsNotFound when either does not exist.
func (c *Client) Reachable(ctx context.Context, owner, repo, ref, branch string) (bool, error) {
	comparison, _, err := c.ghClient.Repositories.CompareCommits(ctx, owner, repo, ref, branch, &github.ListOptions{PerPage: 1})
	if isGithub404(err) {
		return false, microerror.Maskf(notFoundError, "comparison of %s with %s in %s/%s", ref, branch, owner, repo)
	}
	if err != nil {
		return false, microerror.Mask(err)
	}
	switch comparison.GetStatus() {
	case "ahead", "identical":
		return true, nil
	}
	return false, nil
}

// CommitsAfter returns the commits of branch after sha, oldest first: what a
// comparison of sha (base) with branch (head) lists, empty when branch does
// not contain sha. A branch or commit that does not exist answers none.
func (c *Client) CommitsAfter(ctx context.Context, owner, repo, sha, branch string) ([]string, error) {
	opts := &github.ListOptions{PerPage: 100}
	var out []string
	for page := 0; page < maxPages; page++ {
		comparison, resp, err := c.ghClient.Repositories.CompareCommits(ctx, owner, repo, sha, branch, opts)
		if isGithub404(err) {
			return nil, nil
		}
		if err != nil {
			return nil, microerror.Mask(err)
		}
		if comparison.GetStatus() != "ahead" {
			return nil, nil
		}
		for _, commit := range comparison.Commits {
			out = append(out, commit.GetSHA())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// DispatchWorkflow runs the workflow of the file name on ref through its
// workflow_dispatch trigger, with inputs. The request needs Actions write.
func (c *Client) DispatchWorkflow(ctx context.Context, owner, repo, file, ref string, inputs map[string]any) error {
	_, _, err := c.ghClient.Actions.CreateWorkflowDispatchEventByFileName(ctx, owner, repo, file, github.CreateWorkflowDispatchEventRequest{Ref: ref, Inputs: inputs})
	if err != nil {
		return microerror.Mask(err)
	}
	return nil
}
