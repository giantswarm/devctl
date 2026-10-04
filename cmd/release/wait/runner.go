package wait

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

type runner struct {
	flag   *flag
	stdout io.Writer
	stderr io.Writer
	// gate is versiongate.Check: an outdated devctl ends the run in the
	// document.
	gate func(noCache bool) error
	// open is releasewait.OpenSources; tests inject clients over mocks.
	open func(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*releasewait.Sources, error)
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	return r.run(cmd.Context(), args)
}

// FlagError reports a flag cobra could not parse like any other wrong call:
// the document, exit 7.
func (r *runner) FlagError(_ *cobra.Command, err error) error {
	doc := newDocument("")
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictAvailable, agentcli.FlagError(releasewait.Command, err))
}

func (r *runner) run(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	repository := ""
	if len(args) > 0 {
		repository = args[0]
	}
	doc := newDocument(repository)
	err := r.wait(ctx, args, &doc)
	return agentcli.Report(r.stdout, &doc, agentcli.VerdictAvailable, err)
}

func newDocument(repository string) releasewait.Document {
	return releasewait.Document{Envelope: agentcli.NewEnvelope(releasewait.Command, time.Now()), Result: releasewait.NewResult(repository)}
}

func (r *runner) wait(ctx context.Context, args []string, doc *releasewait.Document) error {
	if len(args) == 0 || len(args) > 2 {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "usage: devctl %s <owner/repo> [<vX.Y.Z|X.Y.Z>] [--pr <number>], got %d argument(s)", releasewait.Command, len(args))
	}
	owner, repo, ok := strings.Cut(args[0], "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "%q is not a repository: expected owner/repo", args[0])
	}
	version := ""
	if len(args) > 1 {
		version = args[1]
	}
	if (version == "") == (r.flag.PR == 0) {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "pass exactly one of a version (vX.Y.Z or X.Y.Z) and --pr <number>")
	}
	if version != "" {
		if _, err := releasewait.ParseVersion(version); err != nil {
			return err
		}
	}
	if err := r.gate(false); err != nil {
		return err
	}

	clock, err := agentcli.SystemClock()
	if err != nil {
		return err
	}
	endpoints := agentcli.EndpointsFromEnv()
	progress := agentcli.NewProgress(r.stderr, r.flag.Progress)

	// A read that fails in transit or with a 5xx is sent again, not taken
	// as the wait's outcome.
	retrying := &agentcli.Retrying{Clock: clock, Progress: progress, Warn: doc.Warn}
	progress.Printf("reading the GitHub token from the keychain")
	c, err := r.open(ctx, endpoints, retrying, doc.Warn)
	if err != nil {
		return err
	}

	waiter, err := releasewait.New(releasewait.Config{
		Owner:        owner,
		Repo:         repo,
		Version:      version,
		PR:           r.flag.PR,
		Timeout:      r.flag.Timeout,
		Catalog:      r.flag.Catalog,
		Images:       r.flag.Images,
		Charts:       r.flag.Charts,
		GitHub:       c.GitHub,
		Entries:      c.Entries,
		CircleCI:     c.CircleCI,
		Registry:     c.Registry,
		CatalogIndex: c.CatalogIndex,
		Endpoints:    endpoints,
		Clock:        clock,
		Rate:         c.Rate,
		Progress:     progress,
		Warn:         doc.Warn,
	})
	if err != nil {
		return err
	}
	return waiter.Wait(ctx, &doc.Result)
}
