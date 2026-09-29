package promote

import (
	"context"
	"fmt"
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
	// open is releasepromote.OpenSources; tests inject clients over mocks.
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
	if (len(args) == 0) == (r.flag.Team == "") {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "usage: devctl %s (<owner/repo>... | --team <team>): pass exactly one of repositories and --team, got %d repository argument(s) and --team %q", releasepromote.Command, len(args), r.flag.Team)
	}
	for _, arg := range args {
		if _, _, ok := releasepromote.SplitRepository(arg); !ok {
			return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "%q is not a repository: expected owner/repo", arg)
		}
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

	if sources.DispatchBlocked != "" && !r.flag.DryRun {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "%s; --dry-run checks the candidates without dispatching", sources.DispatchBlocked)
	}

	repositories := unique(args)
	if r.flag.Team != "" {
		progress.Printf("listing the auto-release repositories of %s", r.flag.Team)
		repositories, err = sources.Team(ctx, r.flag.Team)
		if err != nil {
			return err
		}
		if len(repositories) == 0 {
			doc.Warn(fmt.Sprintf("the team file of %s declares no auto-release repository: nothing to promote", r.flag.Team))
		}
	}

	return releasepromote.Promote(ctx, releasepromote.Config{
		GitHub:       sources.GitHub,
		DryRun:       r.flag.DryRun,
		NotFoundHint: sources.NotFoundHint,
		Progress:     progress,
	}, repositories, &doc.Result)
}

// unique is repositories without repeats, in first-seen order.
func unique(repositories []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		if seen[repository] {
			continue
		}
		seen[repository] = true
		out = append(out, repository)
	}
	return out
}
