package merge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
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
	// The seams tests replace: the token gates, the endpoints and the clock.
	requireGitHub   func(ctx context.Context) (authstore.Token, error)
	requireCircleCI func(ctx context.Context) (authstore.Token, error)
	endpoints       func() agentcli.Endpoints
	clock           func() (agentcli.Clock, error)
}

// document is the command's JSON: the envelope and the merge's result.
type document struct {
	agentcli.Envelope
	*prmerge.Result
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
	doc := r.newDocument()
	err := r.merge(ctx, args, &doc)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, err)
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

func (r *runner) merge(ctx context.Context, args []string, doc *document) error {
	owner, repo, number, err := parseArgs(args)
	if err != nil {
		return err
	}
	doc.Repository, doc.Number = owner+"/"+repo, number
	if r.flag.Timeout <= 0 {
		return fmt.Errorf("--%s must be positive, got %s", flagTimeout, r.flag.Timeout)
	}
	if !r.flag.NoReleaseWait && r.flag.ReleaseTimeout <= 0 {
		return fmt.Errorf("--%s must be positive, got %s", flagReleaseTimeout, r.flag.ReleaseTimeout)
	}
	if err := r.gate(false); err != nil {
		return err
	}
	clock, err := r.clock()
	if err != nil {
		return err
	}

	// The gate comes before the first request.
	token, err := r.requireGitHub(ctx)
	if err != nil {
		return err
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
			GitHub:   github,
			Rate:     conditional,
			CircleCI: openCircleCI,
			Clock:    clock,
			Progress: progress,
			Timeout:  r.flag.Timeout,
		},
		Method:       r.method(),
		UpdateBranch: r.flag.UpdateBranch,
		Login:        token.Login,
		Release:      release,
	})
	if err != nil {
		return err
	}

	result, err := merger.Merge(ctx, owner, repo, number)
	doc.Result = result
	for _, w := range result.Warnings {
		doc.Warn(w)
	}
	return err
}

// parseArgs reads "<owner/repo> <number>".
func parseArgs(args []string) (owner, repo string, number int, err error) {
	if len(args) != 2 {
		return "", "", 0, fmt.Errorf("usage: devctl %s <owner/repo> <number>, got %d argument(s)", command, len(args))
	}
	owner, repo, ok := strings.Cut(args[0], "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", 0, fmt.Errorf("the repository is <owner/repo>, got %q", args[0])
	}
	number, err = strconv.Atoi(args[1])
	if err != nil || number <= 0 {
		return "", "", 0, fmt.Errorf("the pull request number is a positive integer, got %q", args[1])
	}
	return owner, repo, number, nil
}
