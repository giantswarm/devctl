package wait

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

const command = "pr wait"

type runner struct {
	flag   *flag
	stdout io.Writer
	stderr io.Writer
	// gate is versiongate.Check: an outdated devctl ends the run in the
	// document.
	gate func(noCache bool) error
	// The seams tests replace: the token gates (the App login for the App's
	// owners, the person's own gh login for every other owner), the renewal
	// of an App token refused mid-run, the endpoints and the clock.
	requireGitHub   func(ctx context.Context) (authstore.Token, error)
	personGitHub    func(ctx context.Context) (authstore.Token, error)
	requireCircleCI func(ctx context.Context) (authstore.Token, error)
	renewGitHub     func(ctx context.Context, rejected string) (string, error)
	endpoints       func() agentcli.Endpoints
	clock           func() (agentcli.Clock, error)
}

// document is the command's JSON: the envelope and the wait's result.
type document struct {
	agentcli.Envelope
	*prwait.Result
	// Identity is who acted on GitHub: "app", the devctl GitHub App login,
	// or "gh", the person\'s own gh login for an owner the App is not
	// installed on; empty when the run ended before choosing.
	Identity string `json:"identity"`
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return r.run(ctx, args)
}

// FlagError reports a flag cobra could not parse like any other wrong call:
// the document, exit 7.
func (r *runner) FlagError(_ *cobra.Command, err error) error {
	doc := newDocument()
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, agentcli.FlagError(command, err))
}

func (r *runner) run(ctx context.Context, args []string) error {
	doc := newDocument()
	err := r.wait(ctx, args, &doc)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, err)
}

func newDocument() document {
	return document{
		Envelope: agentcli.NewEnvelope(command, time.Now()),
		Result:   &prwait.Result{Checks: []prwait.Check{}, Actions: []prwait.ActionRun{}},
	}
}

func (r *runner) wait(ctx context.Context, args []string, doc *document) error {
	owner, repo, number, err := agentcli.ParsePullRequest(command, args)
	if err != nil {
		return err
	}
	doc.Repository, doc.Number = owner+"/"+repo, number
	if r.flag.Timeout <= 0 {
		return fmt.Errorf("--%s must be positive, got %s", flagTimeout, r.flag.Timeout)
	}
	failedLogLines, err := r.flag.FailedLog.TailLines()
	if err != nil {
		return err
	}
	if err := r.gate(false); err != nil {
		return err
	}
	clock, err := r.clock()
	if err != nil {
		return err
	}

	// The gate comes before the first request.
	token, err := authexec.RepositoryToken(ctx, owner, r.requireGitHub, r.personGitHub)
	if err != nil {
		return err
	}
	doc.Identity = authexec.Identity(token)
	// Only the App login renews itself; a refused gh login is the outcome.
	renew := r.renewGitHub
	if doc.Identity != authexec.IdentityApp {
		renew = nil
	}
	endpoints := r.endpoints()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	progress := agentcli.NewProgress(r.stderr, r.flag.Progress)
	// A read that fails in transit or with a 5xx is sent again, not taken
	// as the wait's outcome.
	retrying := &agentcli.Retrying{Clock: clock, Progress: progress, Warn: doc.Warn}
	github, conditional, err := githubclient.NewConditional(githubclient.Config{
		Logger:      logger,
		AccessToken: token.Value,
		BaseURL:     endpoints.GitHubAPIURL,
		Transport:   retrying,
		Renew:       renew,
	})
	if err != nil {
		return err
	}

	waiter, err := prwait.New(prwait.Config{
		GitHub: github,
		Rate:   conditional,
		CircleCI: func(ctx context.Context) (*circleciclient.Client, error) {
			token, err := r.requireCircleCI(ctx)
			if err != nil {
				return nil, err
			}
			doc.Warn(token.Warning)
			return circleciclient.New(circleciclient.Config{
				Token:      token.Value,
				BaseURL:    circleciclient.BaseURLFromAPIURL(endpoints.CircleCIAPIURL),
				HTTPClient: &http.Client{Transport: retrying},
				Logger:     logger,
			})
		},
		Clock:          clock,
		Progress:       progress,
		Timeout:        r.flag.Timeout,
		FailedLogLines: failedLogLines,
	})
	if err != nil {
		return err
	}

	result, err := waiter.Wait(ctx, owner, repo, number)
	doc.Result = result
	for _, w := range result.Warnings {
		doc.Warn(w)
	}
	prwait.PrintFailedJobs(r.stderr, result.FailedJobs)
	return githubclient.ExplainNotFound(err, authexec.NotFoundHint(token, owner))
}
