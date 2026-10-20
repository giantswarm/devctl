package jobs

import (
	"context"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/ci"
	"github.com/giantswarm/devctl/v8/pkg/rerun"
)

const command = ci.CommandJobs

type runner struct {
	stdout io.Writer
	// gate is versiongate.Check: an outdated devctl ends the run in the
	// document.
	gate func(noCache bool) error
	// The seams tests replace: the token gate, the endpoints and the clock.
	requireCircleCI func(ctx context.Context) (authstore.Token, error)
	endpoints       func() agentcli.Endpoints
	clock           func() (agentcli.Clock, error)
}

// document is the command's JSON: the envelope and the view.
type document struct {
	agentcli.Envelope
	*ci.JobsResult
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	doc := r.newDocument(args)
	err := r.jobs(ctx, args, &doc)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictListed, err)
}

// FlagError reports a flag cobra could not parse like any other wrong call:
// the document, exit 7.
func (r *runner) FlagError(_ *cobra.Command, err error) error {
	doc := r.newDocument(nil)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictListed, agentcli.FlagError(command, err))
}

func (r *runner) newDocument(args []string) document {
	repository := ""
	if len(args) > 0 {
		repository = args[0]
	}
	return document{Envelope: agentcli.NewEnvelope(command, time.Now()), JobsResult: ci.NewJobsResult(repository)}
}

func (r *runner) jobs(ctx context.Context, args []string, doc *document) error {
	owner, repo, pipeline, err := agentcli.ParseRepositoryArgument(command, "pipeline number|id", args)
	if err != nil {
		return err
	}
	doc.Repository = owner + "/" + repo
	if err := r.gate(false); err != nil {
		return err
	}
	clock, err := r.clock()
	if err != nil {
		return err
	}
	circleci, err := rerun.NewCircleCI(ctx, r.requireCircleCI, r.endpoints(), &agentcli.Retrying{Warn: doc.Warn, Clock: clock}, doc.Warn)
	if err != nil {
		return err
	}
	return ci.Jobs(ctx, circleci, owner, repo, pipeline, clock.Now(), doc.JobsResult, doc.Warn)
}
