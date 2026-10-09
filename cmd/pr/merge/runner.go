package merge

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
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

const command = "pr merge"

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
	// jobRoot is where detached merges live, executable the devctl
	// --detach starts; tests replace both.
	jobRoot    func() (string, error)
	executable func() (string, error)
}

// document is the command's JSON: the envelope and the merge's result.
type document struct {
	agentcli.Envelope
	*prmerge.Result
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
	doc := r.newDocument()
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, agentcli.FlagError(command, err))
}

func (r *runner) run(ctx context.Context, args []string) error {
	if r.flag.Detach {
		doc := r.newStartDocument()
		err := r.detach(ctx, args, &doc)
		return agentcli.Report(r.stdout, &doc, agentcli.VerdictDetached, err)
	}
	doc := r.newDocument()
	err := r.merge(ctx, args, &doc)
	if r.flag.DetachedHandle == "" {
		return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, err)
	}
	return r.finishDetached(ctx, &doc, err)
}

func (r *runner) newDocument() document {
	return document{
		Envelope: agentcli.NewEnvelope(command, time.Now()),
		Result: &prmerge.Result{
			Result: prwait.Result{Checks: []prwait.Check{}, Actions: []prwait.ActionRun{}},
			Method: string(r.method()),
		},
	}
}

func (r *runner) method() githubclient.MergeMethod {
	if r.flag.Rebase {
		return githubclient.MergeRebase
	}
	return githubclient.MergeSquash
}

// call is a merge's arguments and flags, checked, and the token it acts with.
type call struct {
	owner, repo    string
	number         int
	failedLogLines int
	dispatch       *prmerge.Dispatch
	token          authstore.Token
}

// check reads and checks the arguments and flags, then the version gate and
// the token: everything that can refuse a merge before its first request, so
// --detach refuses the same calls at once.
func (r *runner) check(ctx context.Context, args []string, repository *string, number *int, identity *string) (call, error) {
	var c call
	var err error
	c.owner, c.repo, c.number, err = agentcli.ParsePullRequest(command, args)
	if err != nil {
		return c, err
	}
	*repository, *number = c.owner+"/"+c.repo, c.number
	if r.flag.Timeout <= 0 {
		return c, fmt.Errorf("--%s must be positive, got %s", flagTimeout, r.flag.Timeout)
	}
	if !r.flag.NoReleaseWait && r.flag.ReleaseTimeout <= 0 {
		return c, fmt.Errorf("--%s must be positive, got %s", flagReleaseTimeout, r.flag.ReleaseTimeout)
	}
	if r.flag.OnDone != "" && !r.flag.Detach && r.flag.DetachedHandle == "" {
		return c, fmt.Errorf("--%s needs --%s: a blocking merge reports its outcome itself", flagOnDone, flagDetach)
	}
	if c.failedLogLines, err = r.flag.FailedLog.TailLines(); err != nil {
		return c, err
	}
	if c.dispatch, err = r.flag.dispatch(); err != nil {
		return c, err
	}
	if err := r.gate(false); err != nil {
		return c, err
	}
	// The gate comes before the first request.
	if c.token, err = authexec.RepositoryToken(ctx, c.owner, r.requireGitHub, r.personGitHub); err != nil {
		return c, err
	}
	*identity = authexec.Identity(c.token)
	return c, nil
}

func (r *runner) merge(ctx context.Context, args []string, doc *document) error {
	c, err := r.check(ctx, args, &doc.Repository, &doc.Number, &doc.Identity)
	if err != nil {
		return err
	}
	owner, repo, number, failedLogLines, dispatch, token := c.owner, c.repo, c.number, c.failedLogLines, c.dispatch, c.token
	clock, err := r.clock()
	if err != nil {
		return err
	}
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
	// as the outcome of either wait; the merge itself is never repeated.
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

	// One CircleCI client for the CI wait and the release wait, opened
	// through the gate when either first needs it.
	var circleci *circleciclient.Client
	openCircleCI := func(ctx context.Context) (*circleciclient.Client, error) {
		if circleci != nil {
			return circleci, nil
		}
		token, err := r.requireCircleCI(ctx)
		if err != nil {
			return nil, err
		}
		doc.Warn(token.Warning)
		circleci, err = circleciclient.New(circleciclient.Config{
			Token:      token.Value,
			BaseURL:    circleciclient.BaseURLFromAPIURL(endpoints.CircleCIAPIURL),
			HTTPClient: &http.Client{Transport: retrying},
			Logger:     logger,
		})
		return circleci, err
	}

	var release prmerge.ReleaseWait
	if !r.flag.NoReleaseWait {
		release = func(ctx context.Context, owner, repo string, number int, mergeCommitSHA string, result *releasewait.Result) error {
			waiter, err := releasewait.New(releasewait.Config{
				Owner:          owner,
				Repo:           repo,
				PR:             number,
				MergeCommitSHA: mergeCommitSHA,
				Timeout:        r.flag.ReleaseTimeout,
				GitHub:         github,
				Entries:        releasewait.TeamFileEntries{GitHub: github.GitHub()},
				CircleCI: func(ctx context.Context) (releasewait.CircleCI, error) {
					client, err := openCircleCI(ctx)
					if err != nil {
						return nil, err
					}
					return client, nil
				},
				Registry:  releasewait.RegistryProber{Endpoints: endpoints},
				Endpoints: endpoints,
				Clock:     clock,
				Rate:      conditional.Rate,
				Progress:  progress,
				Warn:      doc.Warn,
			})
			if err != nil {
				return err
			}
			return waiter.Wait(ctx, result)
		}
	}

	merger, err := prmerge.New(prmerge.Config{
		Wait: prwait.Config{
			GitHub:         github,
			Rate:           conditional,
			CircleCI:       openCircleCI,
			Clock:          clock,
			Progress:       progress,
			Timeout:        r.flag.Timeout,
			FailedLogLines: failedLogLines,
		},
		Method:       r.method(),
		UpdateBranch: r.flag.UpdateBranch,
		Login:        token.Login,
		Release:      release,
		Dispatch:     dispatch,
	})
	if err != nil {
		return err
	}

	result, err := merger.Merge(ctx, owner, repo, number)
	doc.Result = result
	for _, w := range result.Warnings {
		doc.Warn(w)
	}
	prwait.PrintFailedJobs(r.stderr, result.FailedJobs)
	return githubclient.ExplainNotFound(err, authexec.NotFoundHint(token, owner))
}
