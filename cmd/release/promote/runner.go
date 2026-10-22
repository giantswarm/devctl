package promote

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

type runner struct {
	flag   *flag
	stdout io.Writer
	stderr io.Writer
	// gate is versiongate.Check: an outdated devctl ends the run in the
	// document.
	gate func(noCache bool) error
	// open is laptop.ReleasePromoteSources; tests inject clients over mocks.
	open func(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*releasepromote.Sources, error)
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	return r.run(cmd.Context(), args)
}

// FlagError reports a flag cobra could not parse like any other wrong call:
// the document, exit 7.
func (r *runner) FlagError(_ *cobra.Command, err error) error {
	doc := newDocument(r.flag)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, agentcli.FlagError(releasepromote.Command, err))
}

func (r *runner) run(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	doc := newDocument(r.flag)
	err := r.promote(ctx, args, &doc)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, err)
}

func newDocument(f *flag) releasepromote.Document {
	result := releasepromote.NewResult(f.Team)
	result.DryRun = f.DryRun
	return releasepromote.Document{Envelope: agentcli.NewEnvelope(releasepromote.Command, time.Now()), Result: result}
}

func (r *runner) promote(ctx context.Context, args []string, doc *releasepromote.Document) error {
	call := releasepromote.Call{
		Repositories:  args,
		Team:          r.flag.Team,
		DryRun:        r.flag.DryRun,
		Version:       r.flag.Version,
		Branch:        r.flag.Branch,
		Follow:        !r.flag.NoFollow,
		FollowTimeout: r.flag.FollowTimeout,
	}
	if err := call.Check(); err != nil {
		return err
	}
	if err := r.gate(false); err != nil {
		return err
	}

	clock, err := agentcli.SystemClock()
	if err != nil {
		return err
	}
	progress := agentcli.NewProgress(r.stderr, r.flag.Progress)
	// A read that fails in transit or with a 5xx is sent again; a dispatch
	// is never repeated.
	retrying := &agentcli.Retrying{Clock: clock, Progress: progress, Warn: doc.Warn}
	sources, err := r.open(ctx, agentcli.EndpointsFromEnv(), retrying, doc.Warn)
	if err != nil {
		return err
	}
	return call.Run(ctx, sources, clock, progress, doc)
}
