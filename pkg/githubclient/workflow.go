package githubclient

import (
	"context"
	"net/http"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// CombinedStatus is the combined commit status of a ref: the state GitHub
// derives from the latest status of every context, and how many contexts
// reported. With no context GitHub still answers pending.
type CombinedStatus struct {
	State      string
	TotalCount int
}

// GetCombinedStatus returns the combined commit status of ref, a branch, tag
// or SHA. IsNotFound when the ref does not exist.
func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, ref string) (CombinedStatus, error) {
	combined, _, err := c.ghClient.Repositories.GetCombinedStatus(ctx, owner, repo, ref, &github.ListOptions{PerPage: 1})
	if isGithub404(err) {
		return CombinedStatus{}, microerror.Maskf(notFoundError, "ref %s in %s/%s", ref, owner, repo)
	}
	if err != nil {
		return CombinedStatus{}, microerror.Mask(err)
	}
	return CombinedStatus{State: combined.GetState(), TotalCount: combined.GetTotalCount()}, nil
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

// HasWorkflow says whether the repository has the GitHub Actions workflow of
// the file name (the base name under .github/workflows).
func (c *Client) HasWorkflow(ctx context.Context, owner, repo, file string) (bool, error) {
	_, _, err := c.ghClient.Actions.GetWorkflowByFileName(ctx, owner, repo, file)
	if isGithub404(err) {
		return false, nil
	}
	if err != nil {
		return false, microerror.Mask(err)
	}
	return true, nil
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

// IsForbidden says whether err, or an error it wraps, is GitHub's 403.
func IsForbidden(err error) bool {
	return githubStatus(err) == http.StatusForbidden
}
