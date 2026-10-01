package wait

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/dynamic"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

type runner struct {
	flag   *flag
	stdout io.Writer
	stderr io.Writer
	// gate is versiongate.Check: an outdated devctl ends the run in the
	// document.
	gate func(noCache bool) error
	// openRelease is releasewait.OpenSources and openCluster
	// rolloutwait.OpenCluster; tests inject fakes.
	openRelease func(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*releasewait.Sources, error)
	openCluster func(installation, kubeContext string, wrap func(http.RoundTripper) http.RoundTripper) (dynamic.Interface, error)
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	return r.run(cmd.Context(), args)
}

// FlagError reports a flag cobra could not parse like any other wrong call:
// the document, exit 7.
func (r *runner) FlagError(_ *cobra.Command, err error) error {
	doc := newDocument("", "", "")
	return agentcli.Report(r.stdout, &doc, rolloutwait.VerdictRolledOut, agentcli.FlagError(rolloutwait.Command, err))
}

func (r *runner) run(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	installation, repository := "", ""
	if len(args) > 0 {
		installation = args[0]
	}
	if len(args) > 1 {
		repository = args[1]
	}
	doc := newDocument(installation, r.kubeContext(installation), repository)
	err := r.wait(ctx, args, &doc)
	return agentcli.Report(r.stdout, &doc, rolloutwait.VerdictRolledOut, err)
}

func newDocument(installation, kubeContext, repository string) rolloutwait.Document {
	doc := rolloutwait.Document{Envelope: agentcli.NewEnvelope(rolloutwait.Command, time.Now()), Result: rolloutwait.NewResult(installation, kubeContext)}
	doc.Release.Result = releasewait.NewResult(repository)
	return doc
}

func (r *runner) kubeContext(installation string) string {
	if r.flag.Context != "" {
		return r.flag.Context
	}
	if installation == "" {
		return ""
	}
	return rolloutwait.ContextPrefix + installation
}

func (r *runner) wait(ctx context.Context, args []string, doc *rolloutwait.Document) error {
	if len(args) < 2 || len(args) > 3 {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "usage: devctl %s <installation> <owner/repo> [<vX.Y.Z|X.Y.Z>] [--pr <number>], got %d argument(s)", rolloutwait.Command, len(args))
	}
	installation := args[0]
	if installation == "" || strings.ContainsAny(installation, "/ ") {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "%q is not an installation name", installation)
	}
	owner, repo, ok := strings.Cut(args[1], "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "%q is not a repository: expected owner/repo", args[1])
	}
	version := ""
	if len(args) > 2 {
		version = args[2]
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
	// as the wait's outcome; writes (--reconcile) pass through once.
	retrying := func(base http.RoundTripper) http.RoundTripper {
		return &agentcli.Retrying{Base: base, Clock: clock, Progress: progress, Warn: doc.Warn}
	}

	// The release first: it names the charts and proves them pullable.
	progress.Printf("reading the GitHub token from the keychain")
	sources, err := r.openRelease(ctx, endpoints, retrying(nil), doc.Warn)
	if err != nil {
		return err
	}
	releaseWaiter, err := releasewait.New(releasewait.Config{
		Owner:        owner,
		Repo:         repo,
		Version:      version,
		PR:           r.flag.PR,
		Timeout:      r.flag.ReleaseTimeout,
		Images:       r.flag.Images,
		GitHub:       sources.GitHub,
		Entries:      sources.Entries,
		CircleCI:     sources.CircleCI,
		Registry:     sources.Registry,
		CatalogIndex: sources.CatalogIndex,
		Endpoints:    endpoints,
		Clock:        clock,
		Rate:         sources.Rate,
		Progress:     progress,
		Warn:         doc.Warn,
	})
	if err != nil {
		return err
	}
	if err := releaseWaiter.Wait(ctx, &doc.Release.Result); err != nil {
		code, verdict := agentcli.Outcome(err)
		doc.Release.Verdict, doc.Release.Reason = verdict, err.Error()
		return agentcli.NewExitError(code, verdict, "release: %s", err)
	}
	doc.Release.Verdict = agentcli.VerdictAvailable

	var charts []string
	for _, a := range doc.Release.Artifacts {
		if a.Kind == releasewait.KindChart {
			charts = append(charts, rolloutwait.ChartName(a.Reference))
		}
	}
	if len(charts) == 0 {
		return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "%s %s ships no chart, nothing rolls out", args[1], doc.Release.Tag)
	}

	kubeContext := r.kubeContext(installation)
	client, err := r.openCluster(installation, kubeContext, retrying)
	if err != nil {
		return err
	}
	waiter, err := rolloutwait.New(rolloutwait.Config{
		Installation: installation,
		KubeContext:  kubeContext,
		Version:      doc.Release.Tag,
		Charts:       charts,
		Client:       client,
		Timeout:      r.flag.Timeout,
		Reconcile:    r.flag.Reconcile,
		Clock:        clock,
		Progress:     progress,
		Warn:         doc.Warn,
	})
	if err != nil {
		return err
	}
	return waiter.Wait(ctx, &doc.Result)
}
