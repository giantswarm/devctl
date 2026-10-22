package wait

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/dynamic"

	"github.com/giantswarm/devctl/v8/internal/laptop"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
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
	// openRelease is laptop.ReleaseWaitSources and openCluster
	// laptop.OpenCluster; tests inject fakes.
	openRelease func(ctx context.Context, endpoints agentcli.Endpoints, transport http.RoundTripper, warn func(string)) (*releasewait.Sources, error)
	openCluster func(ctx context.Context, c laptop.ClusterConfig) (dynamic.Interface, error)
	// teleport logs in to an installation the kubeconfig lacks.
	teleport laptop.Teleport
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
	failedLogLines, err := r.flag.FailedLog.TailLines()
	if err != nil {
		return err
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

	// The kube context is read before the release wait, so a context that
	// does not exist ends the wait at once rather than after the release.
	kubeContext := r.kubeContext(installation)
	client, err := r.openCluster(ctx, laptop.ClusterConfig{
		Installation: installation,
		Context:      kubeContext,
		Kubeconfig:   r.flag.Kubeconfig,
		Wrap:         retrying,
		Teleport:     r.teleport,
		Progress:     progress,
	})
	if err != nil {
		return err
	}

	progress.Printf("reading the GitHub token from the keychain")
	sources, err := r.openRelease(ctx, endpoints, retrying(nil), doc.Warn)
	if err != nil {
		return err
	}
	err = rolloutwait.Release{
		Installation:   installation,
		KubeContext:    kubeContext,
		Owner:          owner,
		Repo:           repo,
		Version:        version,
		PR:             r.flag.PR,
		Timeout:        r.flag.Timeout,
		ReleaseTimeout: r.flag.ReleaseTimeout,
		Images:         r.flag.Images,
		Charts:         r.flag.Charts,
		HelmReleases:   r.flag.HelmReleases,
		Reconcile:      r.flag.Reconcile,
		FailedLogLines: failedLogLines,
		Sources:        sources,
		Client:         client,
		Endpoints:      endpoints,
		Clock:          clock,
		Progress:       progress,
		Warn:           doc.Warn,
	}.Wait(ctx, doc)
	prwait.PrintFailedJobs(r.stderr, doc.Release.FailedJobs)
	return err
}
