package wait

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
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
	// The seams tests replace: the token gates, the endpoints and the clock.
	requireGitHub   func(ctx context.Context) (authstore.Token, error)
	requireCircleCI func(ctx context.Context) (authstore.Token, error)
	endpoints       func() agentcli.Endpoints
	clock           func() (agentcli.Clock, error)
}

// document is the command's JSON: the envelope and the wait's result.
type document struct {
	agentcli.Envelope
	*prwait.Result
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return r.run(ctx, args)
}

func (r *runner) run(ctx context.Context, args []string) error {
	doc := document{
		Envelope: agentcli.NewEnvelope(command, time.Now()),
		Result:   &prwait.Result{Checks: []prwait.Check{}, Actions: []prwait.ActionRun{}},
	}
	err := r.wait(ctx, args, &doc)
	doc.Finish(time.Now(), agentcli.VerdictGreen, err)
	if err := agentcli.Emit(r.stdout, doc); err != nil {
		return err
	}
	return doc.Err()
}

func (r *runner) wait(ctx context.Context, args []string, doc *document) error {
	owner, repo, number, err := parseArgs(args)
	if err != nil {
		return err
	}
	doc.Repository, doc.Number = owner+"/"+repo, number
	if r.flag.Timeout <= 0 {
		return fmt.Errorf("--%s must be positive, got %s", flagTimeout, r.flag.Timeout)
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
	github, conditional, err := githubclient.NewConditional(githubclient.Config{
		Logger:      logger,
		AccessToken: token.Value,
		BaseURL:     endpoints.GitHubAPIURL,
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
				Token:   token.Value,
				BaseURL: strings.TrimSuffix(endpoints.CircleCIAPIURL, "/api/v2"),
				Logger:  logger,
			})
		},
		Clock:    clock,
		Progress: agentcli.NewProgress(r.stderr, r.flag.Progress),
		Timeout:  r.flag.Timeout,
	})
	if err != nil {
		return err
	}

	result, err := waiter.Wait(ctx, owner, repo, number)
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
