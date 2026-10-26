package githubclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// maxPages bounds every paged listing here: a head with more than 1000 check
// runs or statuses is not one the merge box shows either.
const maxPages = 10

// PullRequest returns the pull request as GitHub sees it now: state, draft,
// mergeable state, head and base.
func (c *Client) PullRequest(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	pr, _, err := c.ghClient.PullRequests.Get(ctx, owner, repo, number)
	if isGithub404(err) {
		return nil, microerror.Maskf(notFoundError, "pull request %s/%s#%d", owner, repo, number)
	}
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return pr, nil
}

// PullRequestFiles returns the paths a pull request changes, both sides of
// a rename; GitHub lists at most 3000.
func (c *Client) PullRequestFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	var paths []string
	opts := &github.ListOptions{PerPage: 100}
	for {
		files, resp, err := c.ghClient.PullRequests.ListFiles(ctx, owner, repo, number, opts)
		if isGithub404(err) {
			return nil, microerror.Maskf(notFoundError, "pull request %s/%s#%d", owner, repo, number)
		}
		if err != nil {
			return nil, microerror.Mask(err)
		}
		for _, f := range files {
			paths = append(paths, f.GetFilename())
			if previous := f.GetPreviousFilename(); previous != "" {
				paths = append(paths, previous)
			}
		}
		if resp.NextPage == 0 {
			return paths, nil
		}
		opts.Page = resp.NextPage
	}
}

// IsPullRequest reports whether number in owner/repo is a pull request
// rather than an issue: the issues endpoint answers for both and marks a pull
// request. A number that does not exist is notFoundError.
func (c *Client) IsPullRequest(ctx context.Context, owner, repo string, number int) (bool, error) {
	issue, _, err := c.ghClient.Issues.Get(ctx, owner, repo, number)
	if isGithub404(err) {
		return false, microerror.Maskf(notFoundError, "issue or pull request %s/%s#%d", owner, repo, number)
	}
	if err != nil {
		return false, microerror.Mask(err)
	}
	return issue.IsPullRequest(), nil
}

// CheckRunsForRef returns the check runs of ref as the merge box lists them:
// the latest attempt of each (GitHub's filter=latest: a rerun job replaces its
// earlier attempt; runs of one name in different check suites are all
// listed), every page. A ref GitHub does not know is notFoundError.
func (c *Client) CheckRunsForRef(ctx context.Context, owner, repo, ref string) ([]*github.CheckRun, error) {
	filter := "latest"
	opts := &github.ListCheckRunsOptions{
		Filter:      &filter,
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var runs []*github.CheckRun
	for page := 0; page < maxPages; page++ {
		result, resp, err := c.ghClient.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, opts)
		if isGithub404(err) {
			return nil, microerror.Maskf(notFoundError, "check runs of %s/%s@%s", owner, repo, ref)
		}
		if err != nil {
			return nil, microerror.Mask(err)
		}
		runs = append(runs, result.CheckRuns...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return runs, nil
}

// CommitStatuses returns the commit statuses of ref, the latest per context
// (the combined status), every page.
func (c *Client) CommitStatuses(ctx context.Context, owner, repo, ref string) ([]*github.RepoStatus, error) {
	opts := &github.ListOptions{PerPage: 100}
	var statuses []*github.RepoStatus
	for page := 0; page < maxPages; page++ {
		combined, resp, err := c.ghClient.Repositories.GetCombinedStatus(ctx, owner, repo, ref, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		statuses = append(statuses, combined.Statuses...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return statuses, nil
}

// checkRollupQuery reads one page of a commit's statusCheckRollup: the check
// runs of its latest check suites and the latest status per context.
const checkRollupQuery = `query($owner: String!, $name: String!, $oid: GitObjectID!, $after: String) {
  repository(owner: $owner, name: $name) {
    object(oid: $oid) {
      ... on Commit {
        statusCheckRollup {
          contexts(first: 100, after: $after) {
            pageInfo { hasNextPage endCursor }
            nodes {
              __typename
              ... on CheckRun { databaseId name status conclusion url startedAt completedAt checkSuite { databaseId app { slug } } }
              ... on StatusContext { context state targetUrl createdAt }
            }
          }
        }
      }
    }
  }
}`

type checkRollupPage struct {
	Repository *struct {
		Object *struct {
			StatusCheckRollup *struct {
				Contexts struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []checkRollupNode `json:"nodes"`
				} `json:"contexts"`
			} `json:"statusCheckRollup"`
		} `json:"object"`
	} `json:"repository"`
}

// checkRollupNode is a CheckRun or a StatusContext, told by Typename.
type checkRollupNode struct {
	Typename string `json:"__typename"`

	DatabaseID  int64             `json:"databaseId"`
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	Conclusion  string            `json:"conclusion"`
	URL         string            `json:"url"`
	StartedAt   *github.Timestamp `json:"startedAt"`
	CompletedAt *github.Timestamp `json:"completedAt"`
	CheckSuite  *struct {
		DatabaseID int64 `json:"databaseId"`
		App        *struct {
			Slug string `json:"slug"`
		} `json:"app"`
	} `json:"checkSuite"`

	Context   string            `json:"context"`
	State     string            `json:"state"`
	TargetURL string            `json:"targetUrl"`
	CreatedAt *github.Timestamp `json:"createdAt"`
}

// CheckRollup returns what CheckRunsForRef and CommitStatuses return, read
// through GraphQL's statusCheckRollup of the commit sha instead of the REST
// check-runs and combined-status endpoints, every page: the read for when
// those answer 5xx and GraphQL answers. Status, conclusion and state are
// REST's lowercase words; a status context GitHub only expects (a required
// context nothing has reported) is left out, as the combined status leaves it
// out. A commit GitHub does not know is notFoundError.
func (c *Client) CheckRollup(ctx context.Context, owner, repo, sha string) ([]*github.CheckRun, []*github.RepoStatus, error) {
	runs, statuses := []*github.CheckRun{}, []*github.RepoStatus{}
	variables := map[string]any{"owner": owner, "name": repo, "oid": sha}
	for page := 0; page < maxPages; page++ {
		var data checkRollupPage
		if err := c.graphQL(ctx, "statusCheckRollup", checkRollupQuery, variables, &data); err != nil {
			return nil, nil, err
		}
		if data.Repository == nil || data.Repository.Object == nil {
			return nil, nil, microerror.Maskf(notFoundError, "commit %s/%s@%s", owner, repo, sha)
		}
		rollup := data.Repository.Object.StatusCheckRollup
		if rollup == nil {
			break
		}
		for _, node := range rollup.Contexts.Nodes {
			switch node.Typename {
			case "CheckRun":
				run := &github.CheckRun{
					ID:          new(node.DatabaseID),
					Name:        new(node.Name),
					Status:      new(strings.ToLower(node.Status)),
					Conclusion:  new(strings.ToLower(node.Conclusion)),
					HTMLURL:     new(node.URL),
					StartedAt:   node.StartedAt,
					CompletedAt: node.CompletedAt,
				}
				if suite := node.CheckSuite; suite != nil {
					run.CheckSuite = &github.CheckSuite{ID: new(suite.DatabaseID)}
					if suite.App != nil {
						run.App = &github.App{Slug: new(suite.App.Slug)}
					}
				}
				runs = append(runs, run)
			case "StatusContext":
				if node.State == "EXPECTED" {
					continue
				}
				statuses = append(statuses, &github.RepoStatus{
					Context:   new(node.Context),
					State:     new(strings.ToLower(node.State)),
					TargetURL: new(node.TargetURL),
					UpdatedAt: node.CreatedAt,
				})
			}
		}
		if !rollup.Contexts.PageInfo.HasNextPage {
			break
		}
		variables["after"] = rollup.Contexts.PageInfo.EndCursor
	}
	return runs, statuses, nil
}

// WorkflowRunsForSHA returns every GitHub Actions run of the head SHA,
// whatever its status, every page.
func (c *Client) WorkflowRunsForSHA(ctx context.Context, owner, repo, sha string) ([]*github.WorkflowRun, error) {
	opts := &github.ListWorkflowRunsOptions{
		HeadSHA:     sha,
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var runs []*github.WorkflowRun
	for page := 0; page < maxPages; page++ {
		result, resp, err := c.ghClient.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		runs = append(runs, result.WorkflowRuns...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return runs, nil
}

// RequiredStatusContexts returns the status contexts branch requires before a
// merge: the required status checks of its branch protection and of every
// ruleset in effect on it, sorted and without duplicates. A protection the
// token may not read (404: none, or 403: no admin access) contributes
// nothing; the rules endpoint answers for everyone with read access.
// go-github answers a branch without classic protection with the bare
// ErrBranchNotProtected, which carries no status: a branch guarded by
// rulesets only.
func (c *Client) RequiredStatusContexts(ctx context.Context, owner, repo, branch string) ([]string, error) {
	set := map[string]bool{}

	protection, _, err := c.ghClient.Repositories.GetBranchProtection(ctx, owner, repo, branch)
	switch status := githubStatus(err); {
	case err == nil:
		if protection.RequiredStatusChecks != nil {
			if protection.RequiredStatusChecks.Contexts != nil {
				for _, name := range *protection.RequiredStatusChecks.Contexts {
					set[name] = true
				}
			}
			if protection.RequiredStatusChecks.Checks != nil {
				for _, check := range *protection.RequiredStatusChecks.Checks {
					set[check.Context] = true
				}
			}
		}
	case errors.Is(err, github.ErrBranchNotProtected), status == http.StatusNotFound, status == http.StatusForbidden:
	default:
		return nil, microerror.Mask(err)
	}

	rules, _, err := c.ghClient.Repositories.ListRulesForBranch(ctx, owner, repo, branch, &github.ListOptions{PerPage: 100})
	switch status := githubStatus(err); {
	case err == nil:
		for _, rule := range rules.RequiredStatusChecks {
			for _, check := range rule.Parameters.RequiredStatusChecks {
				set[check.Context] = true
			}
		}
	case status == http.StatusNotFound, status == http.StatusForbidden:
	default:
		return nil, microerror.Mask(err)
	}

	contexts := make([]string, 0, len(set))
	for name := range set {
		contexts = append(contexts, name)
	}
	sort.Strings(contexts)
	return contexts, nil
}

const reviewDecisionQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) { reviewDecision }
  }
}`

// ReviewDecision returns GitHub's review decision of the pull request:
// APPROVED, CHANGES_REQUESTED or REVIEW_REQUIRED, empty when the base
// requires no review. A pull request GitHub does not know is notFoundError.
func (c *Client) ReviewDecision(ctx context.Context, owner, repo string, number int) (string, error) {
	var data struct {
		Repository *struct {
			PullRequest *struct {
				ReviewDecision string `json:"reviewDecision"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	variables := map[string]any{"owner": owner, "name": repo, "number": number}
	if err := c.graphQL(ctx, "reviewDecision", reviewDecisionQuery, variables, &data); err != nil {
		return "", err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return "", microerror.Maskf(notFoundError, "pull request %s/%s#%d", owner, repo, number)
	}
	return data.Repository.PullRequest.ReviewDecision, nil
}

// FileExists says whether path is a file at ref.
func (c *Client) FileExists(ctx context.Context, owner, repo, path, ref string) (bool, error) {
	file, _, _, err := c.ghClient.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
	if isGithub404(err) {
		return false, nil
	}
	if err != nil {
		return false, microerror.Mask(err)
	}
	return file != nil, nil
}

// githubStatus is the HTTP status of a go-github error, 0 for any other.
func githubStatus(err error) int {
	var errResponse *github.ErrorResponse
	if errors.As(err, &errResponse) && errResponse.Response != nil {
		return errResponse.Response.StatusCode
	}
	return 0
}

// JobLog returns the log of a GitHub Actions job; the check run an Actions
// job reports under carries the job's id. GitHub answers with a redirect to
// a signed URL, which is read without the token. The caller closes it.
func (c *Client) JobLog(ctx context.Context, owner, repo string, jobID int64) (io.ReadCloser, error) {
	location, _, err := c.ghClient.Actions.GetWorkflowJobLogs(ctx, owner, repo, jobID, 1)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	resp, err := c.download.Do(req)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, microerror.Maskf(executionError, "job %d log: HTTP %d", jobID, resp.StatusCode)
	}
	return resp.Body, nil
}

// CommitMessage returns the message of a commit, read through the git data
// API (no file list).
func (c *Client) CommitMessage(ctx context.Context, owner, repo, sha string) (string, error) {
	commit, _, err := c.ghClient.Git.GetCommit(ctx, owner, repo, sha)
	if err != nil {
		return "", microerror.Mask(err)
	}
	return commit.GetMessage(), nil
}

// InactiveWorkflowPaths returns the paths of the repository's GitHub Actions
// workflows that are not active (disabled manually, for inactivity or by a
// fork), every page: no event starts a run of them.
func (c *Client) InactiveWorkflowPaths(ctx context.Context, owner, repo string) (map[string]bool, error) {
	opts := &github.ListOptions{PerPage: 100}
	inactive := map[string]bool{}
	for page := 0; page < maxPages; page++ {
		result, resp, err := c.ghClient.Actions.ListWorkflows(ctx, owner, repo, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		for _, workflow := range result.Workflows {
			if workflow.GetState() != "active" {
				inactive[workflow.GetPath()] = true
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return inactive, nil
}
