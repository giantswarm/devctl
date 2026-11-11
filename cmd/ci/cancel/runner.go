package cancel

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/ci"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	workflowrerun "github.com/giantswarm/devctl/v8/pkg/rerun"
)

const command = "ci cancel"

type runner struct {
	flag   *flag
	stdout io.Writer
	// gate is versiongate.Check: an outdated devctl ends the run in the
	// document.
	gate func(noCache bool) error
	// The seams tests replace: the token gate, the endpoints and the clock.
	requireCircleCI func(ctx context.Context) (authstore.Token, error)
	endpoints       func() agentcli.Endpoints
	clock           func() (agentcli.Clock, error)
}

// document is the command's JSON: the envelope and the cancellation result.
type document struct {
	agentcli.Envelope
	*ci.CancelResult
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	doc := r.newDocument(args)
	err := r.cancel(ctx, args, &doc)
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
	return document{Envelope: agentcli.NewEnvelope(command, time.Now()), CancelResult: &ci.CancelResult{Repository: repository, Workflows: []ci.CancelWorkflow{}, UncancelablePipelines: []string{}}}
}

func (r *runner) cancel(ctx context.Context, args []string, doc *document) error {
	if (r.flag.Stale && (len(args) != 1 || r.flag.Pipeline)) || (!r.flag.Stale && len(args) != 2) {
		return fmt.Errorf("usage: devctl ci cancel <owner/repo> <workflow id> [--pipeline], or <owner/repo> --stale")
	}
	owner, repo, err := agentcli.ParseRepository(args[0])
	if err != nil {
		return err
	}
	doc.Repository = owner + "/" + repo
	doc.DryRun = r.flag.DryRun
	if err := r.gate(false); err != nil {
		return err
	}
	clock, err := r.clock()
	if err != nil {
		return err
	}
	endpoints := r.endpoints()
	client, err := workflowrerun.NewCircleCI(ctx, r.requireCircleCI, endpoints, &agentcli.Retrying{Warn: doc.Warn, Clock: clock}, doc.Warn)
	if err != nil {
		return err
	}
	if !r.flag.Stale {
		return ci.CancelOne(ctx, client, owner, repo, args[1], r.flag.Pipeline, doc.CancelResult)
	}
	token, err := authstore.RequireGitHub(ctx)
	if err != nil {
		return err
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	gh, err := githubclient.New(githubclient.Config{Logger: logger, AccessToken: token.Value, BaseURL: endpoints.GitHubAPIURL, Transport: &agentcli.Retrying{Warn: doc.Warn, Clock: clock}})
	if err != nil {
		return err
	}
	return ci.CancelStale(ctx, client, owner, repo, func(ctx context.Context, p circleciclient.Pipeline) (bool, error) {
		return gh.StalePipeline(ctx, owner, repo, p)
	}, doc.CancelResult)
}
