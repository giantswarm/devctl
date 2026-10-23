package rolloutwait

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/client-go/dynamic"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// Release is one rollout wait of a release, the CLI's and the server's:
// the release wait first, which names the release's charts and proves them
// pullable, then the wait on the installation. A pull request that releases
// nothing (a configuration change) is followed as its merge commit.
type Release struct {
	// Installation is the installation's name; KubeContext names how it is
	// read, in the document and in refusals.
	Installation string
	KubeContext  string
	// Owner and Repo are the repository; exactly one of Version and PR
	// names the release.
	Owner, Repo string
	Version     string
	PR          int
	// Timeout bounds the rollout, ReleaseTimeout the release wait first.
	Timeout        time.Duration
	ReleaseTimeout time.Duration
	// Deadline, when set, ends the rollout by then whatever Timeout says:
	// a caller with one budget for both waits.
	Deadline time.Time
	// Images and Charts are waited for besides the ones the repository
	// declares; HelmReleases are followed whatever chart they deploy.
	Images       []string
	Charts       []string
	HelmReleases []string
	// Reconcile asks Flux to reconcile what is behind once.
	Reconcile bool
	// FailedLogLines reads the tail of a failed tag pipeline's log.
	FailedLogLines int
	// CircleCIFromStatuses reads the tag pipeline from the commit
	// statuses, for a caller without a CircleCI credential; the Sources'
	// CircleCI is not used then.
	CircleCIFromStatuses bool
	// SignIn is what a refused sign-in tells the caller to do; empty is
	// tsh kube login on a laptop.
	SignIn string
	// Sources read the release; Client reads the management cluster.
	Sources   *releasewait.Sources
	Client    dynamic.Interface
	Endpoints agentcli.Endpoints

	Clock    agentcli.Clock
	Progress *agentcli.Progress
	Warn     func(message string)
}

// Wait runs the release wait, then the rollout, into doc.
func (r Release) Wait(ctx context.Context, doc *Document) error {
	circleCI := r.Sources.CircleCI
	if r.CircleCIFromStatuses {
		circleCI = nil
	}
	releaseWaiter, err := releasewait.New(releasewait.Config{
		Owner:   r.Owner,
		Repo:    r.Repo,
		Version: r.Version,
		PR:      r.PR,
		// An installation follows the stable release a promote cuts from
		// the candidate on the merge commit, not the candidate.
		PreferPromoted:       true,
		Timeout:              r.ReleaseTimeout,
		Images:               r.Images,
		Charts:               r.Charts,
		FailedLogLines:       r.FailedLogLines,
		GitHub:               r.Sources.GitHub,
		Entries:              r.Sources.Entries,
		CircleCI:             circleCI,
		CircleCIFromStatuses: r.CircleCIFromStatuses,
		Registry:             r.Sources.Registry,
		CatalogIndex:         r.Sources.CatalogIndex,
		Endpoints:            r.Endpoints,
		Clock:                r.Clock,
		Rate:                 r.Sources.Rate,
		Progress:             r.Progress,
		Warn:                 r.Warn,
	})
	if err != nil {
		return err
	}
	if err := releaseWaiter.Wait(ctx, &doc.Release.Result); err != nil {
		code, verdict := agentcli.Outcome(err)
		doc.Release.Verdict, doc.Release.Reason = verdict, err.Error()
		// A pull request that releases nothing (a configuration change)
		// rolls out as its merge commit, through what fetches the
		// repository on the installation.
		if releasewait.IsNoRelease(err) && r.PR != 0 && doc.Release.SHA != "" {
			return r.waitRevision(ctx, doc, err)
		}
		return agentcli.NewExitError(code, verdict, "release: %s", err)
	}
	doc.Release.Verdict = agentcli.VerdictAvailable
	// What rolls out is the tag the release wait found: without one,
	// nothing on the installation can be judged, and the wait never ends
	// in a rollout of nothing.
	if doc.Release.Tag == "" {
		return agentcli.NewExitError(agentcli.ExitNotApplicable, VerdictRolloutUnknown, "release: %s/%s is available, but the release wait named no version to roll out", r.Owner, r.Repo)
	}

	var charts []string
	for _, a := range doc.Release.Artifacts {
		if a.Kind == releasewait.KindChart {
			charts = append(charts, ChartName(a.Reference))
		}
	}
	if len(charts) == 0 {
		hint := ""
		if doc.Release.CIModel == releasewait.CIModelHandWritten {
			hint = "; a chart its hand-written pipeline pushes outside the architect orb is named with --chart"
		}
		return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "%s/%s %s ships no chart, nothing rolls out%s", r.Owner, r.Repo, doc.Release.Tag, hint)
	}

	waiter, err := New(r.config(Config{Version: doc.Release.Tag, Charts: charts}))
	if err != nil {
		return err
	}
	err = waiter.Wait(ctx, &doc.Result)
	if doc.Release.PromotePending && agentcli.Exit(err) == agentcli.ExitNotApplicable {
		_, verdict := agentcli.Outcome(err)
		return agentcli.NewExitError(agentcli.ExitNotApplicable, verdict, "%s; %s is the release candidate of #%d and no promoted release contains it yet: the promote is pending (devctl release promote %s/%s), after which --pr follows the promoted release", err, doc.Release.Candidate, r.PR, r.Owner, r.Repo)
	}
	return err
}

// waitRevision follows the merge commit of a pull request that releases
// nothing to what applies it on the installation; when nothing there
// fetches the repository, the answer stays that no release follows.
func (r Release) waitRevision(ctx context.Context, doc *Document, noRelease error) error {
	client := r.Sources.Client
	if client == nil {
		return fmt.Errorf("release: %w; following the merge commit needs the GitHub client", noRelease)
	}
	r.Progress.Printf("%s; following the merge commit %s on %s", noRelease, doc.Release.SHA, r.Installation)
	paths, err := client.PullRequestFiles(ctx, r.Owner, r.Repo, r.PR)
	if err != nil {
		return fmt.Errorf("listing the files of %s/%s#%d: %w", r.Owner, r.Repo, r.PR, err)
	}
	merge, err := client.GetPullRequestMerge(ctx, r.Owner, r.Repo, r.PR)
	if err != nil {
		return fmt.Errorf("reading %s/%s#%d: %w", r.Owner, r.Repo, r.PR, err)
	}
	waiter, err := New(r.config(Config{
		Revision:   doc.Release.SHA,
		Repository: r.Owner + "/" + r.Repo,
		Paths:      paths,
		GitHub:     client,
		MergedAt:   merge.MergedAt,
	}))
	if err != nil {
		return err
	}
	err = waiter.Wait(ctx, &doc.Result)
	var notFetched *NotFetchedError
	if errors.As(err, &notFetched) {
		return &NotFetchedError{Reason: fmt.Sprintf("release: %s; %s", noRelease, notFetched.Reason)}
	}
	return err
}

// config is c, what the release names, with the rest of r.
func (r Release) config(c Config) Config {
	c.Installation = r.Installation
	c.KubeContext = r.KubeContext
	c.HelmReleases = r.HelmReleases
	c.Client = r.Client
	c.Timeout = r.Timeout
	if !r.Deadline.IsZero() {
		// At least a moment, so the rollout reads the cluster once.
		left := max(r.Deadline.Sub(r.Clock.Now()), time.Second)
		if c.Timeout <= 0 || left < c.Timeout {
			c.Timeout = left
		}
	}
	c.Reconcile = r.Reconcile
	c.SignIn = r.SignIn
	c.Clock = r.Clock
	c.Progress = r.Progress
	c.Warn = r.Warn
	return c
}
