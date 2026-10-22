package prmerge

import (
	"context"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

// GitHub is what a merge reads from and writes to GitHub: the wait's reads
// and the merge's own. *githubclient.Client is one; a test double of this
// interface drives the engine without GitHub.
type GitHub interface {
	prwait.GitHub

	// CurrentLogin is the login the token acts as.
	CurrentLogin(ctx context.Context) (string, error)
	// Repository carries the merge methods the repository allows.
	Repository(ctx context.Context, owner, repo string) (*github.Repository, error)
	MergeQueueRequired(ctx context.Context, owner, repo, branch string) (bool, error)
	BehindBy(ctx context.Context, owner, repo, base, headSHA string) (int, error)

	// The review guard's reads.
	PullRequestReviews(ctx context.Context, owner, repo string, number int) ([]*github.PullRequestReview, error)
	PullRequestReviewComments(ctx context.Context, owner, repo string, number int) ([]*github.PullRequestComment, error)
	IssueComments(ctx context.Context, owner, repo string, number int) ([]*github.IssueComment, error)
	CommitTime(ctx context.Context, owner, repo, sha string) (time.Time, error)
	// IsPullRequest tells a pull request from an issue, for the closing
	// keywords' warnings.
	IsPullRequest(ctx context.Context, owner, repo string, number int) (bool, error)

	// The rules a declined merge is explained by.
	RulesForBranch(ctx context.Context, owner, repo, branch string) (*github.BranchRules, error)
	RepositoryRuleset(ctx context.Context, owner, repo string, id int64) (*github.RepositoryRuleset, error)
	OrganizationRuleset(ctx context.Context, org string, id int64) (*github.RepositoryRuleset, error)

	// The writes.
	UpdatePullRequestBranch(ctx context.Context, owner, repo string, number int, expectedHeadSHA string) error
	RebasePullRequestBranch(ctx context.Context, nodeID, expectedHeadSHA string) error
	MergePullRequest(ctx context.Context, owner, repo string, number int, opts githubclient.MergeOptions) (string, error)
	EnqueuePullRequest(ctx context.Context, nodeID string) error
	DeleteBranch(ctx context.Context, owner, repo, branch string) error

	// The dispatch after the merge.
	DefaultBranch(ctx context.Context, owner, repo string) (string, error)
	DispatchWorkflow(ctx context.Context, owner, repo, file, ref string, inputs map[string]any) error
}

var _ GitHub = &githubclient.Client{}
