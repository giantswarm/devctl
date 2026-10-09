package rerun

import (
	"context"
	"fmt"
	"io"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/rerun"
)

const command = rerun.CommandPR

type runner struct {
	stdout io.Writer
	// gate is versiongate.Check: an outdated devctl ends the run in the
	// document.
	gate func(noCache bool) error
	// The seams tests replace: the token gates and the endpoints.
	requireGitHub   func(ctx context.Context) (authstore.Token, error)
	personGitHub    func(ctx context.Context) (authstore.Token, error)
	requireCircleCI func(ctx context.Context) (authstore.Token, error)
	endpoints       func() agentcli.Endpoints
}

// document is the command's JSON: the rerun's and who read GitHub.
type document struct {
	rerun.Document
	// Identity is who read GitHub: "app" or "gh", as in devctl pr wait;
	// empty when the run ended before choosing.
	Identity string `json:"identity"`
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	doc := r.newDocument(args)
	err := r.rerun(ctx, args, &doc)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, err)
}

// FlagError reports a flag cobra could not parse like any other wrong call:
// the document, exit 7.
func (r *runner) FlagError(_ *cobra.Command, err error) error {
	doc := r.newDocument(nil)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, agentcli.FlagError(command, err))
}

func (r *runner) newDocument(args []string) document {
	repository := ""
	if len(args) > 0 {
		repository = args[0]
	}
	return document{Document: rerun.NewDocument(command, repository)}
}

func (r *runner) rerun(ctx context.Context, args []string, doc *document) error {
	owner, repo, number, err := agentcli.ParsePullRequest(command, args)
	if err != nil {
		return err
	}
	doc.Repository, doc.Number = owner+"/"+repo, number
	if err := r.gate(false); err != nil {
		return err
	}

	token, err := authexec.RepositoryToken(ctx, owner, r.requireGitHub, r.personGitHub)
	if err != nil {
		return err
	}
	doc.Identity = authexec.Identity(token)
	endpoints := r.endpoints()
	retrying := &agentcli.Retrying{Warn: doc.Warn}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	github, _, err := githubclient.NewConditional(githubclient.Config{
		Logger:      logger,
		AccessToken: token.Value,
		BaseURL:     endpoints.GitHubAPIURL,
		Transport:   retrying,
	})
	if err != nil {
		return err
	}
	pr, err := github.PullRequest(ctx, owner, repo, number)
	if err != nil {
		return githubclient.ExplainNotFound(err, authexec.NotFoundHint(token, owner))
	}
	sha := pr.GetHead().GetSHA()
	doc.HeadSHA = sha
	branch := circleciclient.PullRequestBranch(pr.GetHead().GetRef(), prwait.IsFork(pr), number)

	circleci, err := rerun.NewCircleCI(ctx, r.requireCircleCI, endpoints, retrying, doc.Warn)
	if err != nil {
		return err
	}
	pipeline, err := circleci.FindPipelineByRevision(ctx, owner, repo, branch, sha)
	if err != nil {
		return err
	}
	if pipeline == nil {
		return rerun.NoPipeline(fmt.Sprintf("the head %s of %s/%s#%d on branch %s", sha, owner, repo, number, branch))
	}
	return rerun.FromFailed(ctx, circleci, owner, repo, pipeline, doc.Result, doc.Warn)
}
