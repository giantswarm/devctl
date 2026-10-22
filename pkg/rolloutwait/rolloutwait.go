// Package rolloutwait is devctl rollout wait: block until a release's charts
// run on an installation. A chart is deployed by a Flux HelmRelease (from an
// OCIRepository or a HelmChart) or an App CR on the installation's
// management cluster; the wait ends when every one of them has deployed the
// version (or a newer one), is ready, and the Deployments, StatefulSets and
// DaemonSets of its Helm release are rolled out.
package rolloutwait

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// Command is the command's name in its document.
const Command = "rollout wait"

// DefaultTimeout bounds a rollout that gets no --timeout.
const DefaultTimeout = 30 * time.Minute

// The poll interval at scale 1: IntervalFloor after the first read,
// doubling to IntervalCeiling. A handful of list requests against one
// management cluster each time.
const (
	IntervalFloor   = 5 * time.Second
	IntervalCeiling = 30 * time.Second
)

// Config is one rollout wait.
type Config struct {
	// Installation is the installation's name; KubeContext the context it
	// is read through.
	Installation string
	KubeContext  string
	// Version is the release's version, with or without a leading v.
	Version string
	// Charts are the names of the release's charts; empty with Revision.
	Charts []string
	// Revision is, for a pull request that releases nothing (a
	// configuration change), its merge commit in Repository (owner/repo):
	// the wait follows it through the Flux GitRepositories of the
	// repository to what applies them, and the HelmReleases of the apps
	// Paths, the files the pull request changed, touch. GitHub answers
	// whether a fetched commit contains it. Version is empty then.
	// MergedAt is when the pull request merged: no values rendered before
	// it carry the change.
	Revision   string
	Repository string
	Paths      []string
	GitHub     GitHub
	MergedAt   time.Time
	// HelmReleases name HelmReleases to wait for whatever chart they
	// deploy, as <name> (any namespace) or <namespace>/<name>.
	HelmReleases []string
	// Client reads the management cluster.
	Client dynamic.Interface
	// Timeout bounds the wait; zero means DefaultTimeout.
	Timeout time.Duration
	// Reconcile asks Flux to reconcile the sources and HelmReleases that
	// are behind once, instead of waiting for their next interval.
	Reconcile bool

	Clock    agentcli.Clock
	Progress *agentcli.Progress
	Warn     func(message string)
}

// Waiter runs one rollout wait.
type Waiter struct {
	installation, kubeContext, version string
	charts                             map[string]bool
	helmReleaseNames                   []string
	client                             dynamic.Interface
	timeout                            time.Duration
	reconcile                          bool
	clock                              agentcli.Clock
	progress                           *agentcli.Progress
	warn                               func(string)
	// polled are the warnings of the latest poll; announced those already
	// shown as progress.
	polled    []string
	announced map[string]bool

	revision, repository string
	github               GitHub
	mergedAt             time.Time
	// touched are the apps the configuration change touches.
	touched map[string]bool
	// contains keeps whether a commit contains the revision.
	contains map[string]bool
	// requested are the reconcile requests --reconcile made, by object:
	// the requestedAt each carried.
	requested map[string]string
	// seenHelmReleases is how many HelmReleases the last poll listed.
	seenHelmReleases int
	// listed are the lists of this poll, by resource and namespace.
	listed map[string][]unstructured.Unstructured
}

// New validates config.
func New(config Config) (*Waiter, error) {
	if config.Client == nil {
		return nil, errors.New("rolloutwait: Client must not be nil")
	}
	if config.Revision == "" && len(config.Charts) == 0 {
		return nil, errors.New("rolloutwait: Charts must not be empty without a Revision")
	}
	if config.Revision != "" && (config.GitHub == nil || config.Repository == "") {
		return nil, errors.New("rolloutwait: a Revision needs GitHub and Repository")
	}
	for _, name := range config.HelmReleases {
		namespace, n, namespaced := strings.Cut(name, "/")
		if name == "" || strings.Contains(n, "/") || (namespaced && (namespace == "" || n == "")) {
			return nil, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "--helmrelease %q: name a HelmRelease as <name> or <namespace>/<name>", name)
		}
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	if config.Warn == nil {
		config.Warn = func(string) {}
	}
	w := &Waiter{
		installation:     config.Installation,
		kubeContext:      config.KubeContext,
		version:          bare(config.Version),
		charts:           map[string]bool{},
		helmReleaseNames: config.HelmReleases,
		client:           config.Client,
		timeout:          config.Timeout,
		reconcile:        config.Reconcile,
		clock:            config.Clock,
		progress:         config.Progress,
		warn:             config.Warn,
		announced:        map[string]bool{},
		revision:         config.Revision,
		repository:       config.Repository,
		github:           config.GitHub,
		mergedAt:         config.MergedAt,
		contains:         map[string]bool{},
		requested:        map[string]string{},
	}
	if w.revision != "" {
		apps, global := AffectedApps(config.Paths, config.Installation)
		w.touched = apps
		if len(global) > 0 {
			w.warn(fmt.Sprintf("%s change values beyond one app's on %s: a HelmRelease upgrades only where its rendered values changed, which only the values tell, so the HelmReleases of every app are not followed; --helmrelease names one to follow", strings.Join(global, ", "), config.Installation))
		}
	}
	for _, c := range config.Charts {
		w.charts[c] = true
	}
	return w, nil
}

// Wait polls the installation until the release runs there, fails, turns out
// never to get there, or the timeout passes, and fills result with the last
// poll. The error places the outcome in the exit-code table: nil when every
// deployment that follows the version has rolled out, exit 1 when one
// failed, 2 at the timeout, 3 when no deployment of the charts exists,
// none follows the version, or whether a changed app runs the change
// cannot be told. Only the warnings of the last poll reach the result: a
// state seen mid-roll and gone by the end is not reported.
func (w *Waiter) Wait(ctx context.Context, result *Result) error {
	result.Version = w.version
	result.Revision = w.revision
	result.Charts = w.chartNames()
	defer func() {
		for _, m := range w.polled {
			w.warn(m)
		}
	}()
	deadline := w.clock.Now().Add(w.clock.ScaledTimeout(w.timeout))
	interval := IntervalFloor
	reconciled := false
	for {
		w.polled, w.listed = nil, map[string][]unstructured.Unstructured{}
		list, err := w.observe(ctx)
		if err != nil {
			return err
		}
		result.Deployments = list
		if missing := w.missingHelmReleases(list); len(missing) > 0 {
			return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "no HelmRelease %s on %s: none of the %d HelmReleases in all namespaces of %s answers to it as <name> or <namespace>/<name>", strings.Join(missing, ", "), w.installation, w.seenHelmReleases, w.kubeContext)
		}
		if len(list) == 0 {
			return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "no HelmRelease or App on %s deploys %s", w.installation, strings.Join(result.Charts, ", "))
		}
		if err := w.outcome(list); err != nil {
			return err
		}
		if pendingDeployments(list) == nil {
			if unknown := unknownDeployments(list); unknown != nil {
				return agentcli.NewExitError(agentcli.ExitNotApplicable, VerdictRolloutUnknown, "%s is applied on %s, whether it runs cannot be told: %s", w.target(), w.installation, strings.Join(unknown, "; "))
			}
			if w.revision != "" {
				w.progress.Printf("%s@%s is applied on %s", w.repository, short(w.revision), w.installation)
				return nil
			}
			w.progress.Printf("%s runs %s on %s", strings.Join(result.Charts, ", "), w.version, w.installation)
			return nil
		}
		// A revision's objects become worth a request one after another
		// (a HelmRelease once its values are rendered): each is asked
		// once, when it is.
		if w.reconcile && (!reconciled || w.revision != "") {
			if err := w.requestReconciles(ctx, list); err != nil {
				return err
			}
			reconciled = true
		}
		for _, d := range pendingDeployments(list) {
			w.progress.Printf("%s: %s", d.id(), d.Message)
		}
		if !w.clock.Now().Add(w.clock.Scaled(interval)).Before(deadline) {
			return w.timeoutErr(list)
		}
		if err := w.clock.Sleep(ctx, interval); err != nil {
			return err
		}
		interval = min(2*interval, IntervalCeiling)
	}
}

// observe reads every HelmRelease and App that deploys one of the charts,
// or what applies the revision.
func (w *Waiter) observe(ctx context.Context) ([]Deployment, error) {
	if w.revision != "" {
		return w.observeRevision(ctx)
	}
	list, err := w.helmReleases(ctx)
	if err != nil {
		return nil, err
	}
	fromApps, err := w.apps(ctx)
	if err != nil {
		return nil, err
	}
	list = append(list, fromApps...)
	if list == nil {
		list = []Deployment{}
	}
	return list, nil
}

// named reports whether --helmrelease names the HelmRelease.
func (w *Waiter) named(namespace, name string) bool {
	return slices.Contains(w.helmReleaseNames, name) || slices.Contains(w.helmReleaseNames, namespace+"/"+name)
}

// missingHelmReleases are the names --helmrelease gives that no HelmRelease
// on the cluster answers to.
func (w *Waiter) missingHelmReleases(list []Deployment) []string {
	var missing []string
	for _, name := range w.helmReleaseNames {
		if !slices.ContainsFunc(list, func(d Deployment) bool {
			return d.Kind == KindHelmRelease && (d.Name == name || d.Namespace+"/"+d.Name == name)
		}) {
			missing = append(missing, name)
		}
	}
	return missing
}

// outcome is the error of a poll that ends the wait early: a failed
// deployment (exit 1), or none that follows the version (exit 3). A
// deployment that will not follow (a workload cluster's App pinned to an
// older version, a HelmRelease on another range) is reported and warned
// about, and the wait goes on for the others.
func (w *Waiter) outcome(list []Deployment) error {
	for _, d := range list {
		if d.State == StateFailed {
			return agentcli.NewExitError(agentcli.ExitRed, VerdictRolloutFailed, "%s failed on %s: %s", d.id(), w.installation, d.Message)
		}
	}
	var excluded []string
	for _, d := range list {
		if !follows(d) {
			w.warnPoll(fmt.Sprintf("%s will not deploy %s: %s", d.id(), w.target(), d.Message))
			excluded = append(excluded, d.id()+": "+d.Message)
		}
	}
	if len(excluded) == len(list) {
		return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "nothing on %s will deploy %s: %s", w.installation, w.target(), strings.Join(excluded, "; "))
	}
	return nil
}

// follows says whether a deployment gets the version on its own.
func follows(d Deployment) bool {
	return d.State != StateNotFollowing && d.State != StateSuspended
}

func (w *Waiter) requestReconciles(ctx context.Context, list []Deployment) error {
	for _, d := range pendingDeployments(list) {
		if w.revision != "" {
			if d.self == nil || w.requested[d.self.String()] != "" {
				continue
			}
			if err := w.requestReconcile(ctx, *d.self); err != nil {
				return err
			}
			w.progress.Printf("requested a reconcile of %s", d.self)
			continue
		}
		// A named HelmRelease of another chart is not behind the version.
		if d.Kind != KindHelmRelease || !w.charts[d.Chart] || atLeast(d.RunningVersion, w.version) {
			continue
		}
		targets := []object{{resource: helmReleases, kind: KindHelmRelease, namespace: d.Namespace, name: d.Name}}
		if d.source != nil {
			targets = append([]object{*d.source}, targets...)
		}
		for _, o := range targets {
			if err := w.requestReconcile(ctx, o); err != nil {
				return err
			}
			w.progress.Printf("requested a reconcile of %s", o)
		}
	}
	return nil
}

func (w *Waiter) timeoutErr(list []Deployment) error {
	var waiting []string
	for _, d := range pendingDeployments(list) {
		waiting = append(waiting, d.id()+": "+d.Message)
	}
	return agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout, "%s not rolled out on %s after %s: %s", w.target(), w.installation, w.timeout, strings.Join(waiting, "; "))
}

// target names what is waited for: the version, or the revision.
func (w *Waiter) target() string {
	if w.revision != "" {
		return w.repository + "@" + short(w.revision)
	}
	return w.version
}

func pendingDeployments(list []Deployment) []Deployment {
	var p []Deployment
	for _, d := range list {
		if follows(d) && d.State != StateRolledOut && d.State != StateUnknown {
			p = append(p, d)
		}
	}
	return p
}

// unknownDeployments names the deployments whose rollout cannot be told,
// with why.
func unknownDeployments(list []Deployment) []string {
	var u []string
	for _, d := range list {
		if d.State == StateUnknown {
			u = append(u, d.id()+": "+d.Message)
		}
	}
	return u
}

func (w *Waiter) chartNames() []string {
	names := make([]string, 0, len(w.charts))
	for c := range w.charts {
		names = append(names, c)
	}
	slices.Sort(names)
	return names
}

// warnPoll records a warning about what this poll found, shown as progress
// the first time it is seen: every poll judges the same deployments again,
// and the result keeps the last poll's.
func (w *Waiter) warnPoll(message string) {
	if slices.Contains(w.polled, message) {
		return
	}
	w.polled = append(w.polled, message)
	if !w.announced[message] {
		w.announced[message] = true
		w.progress.Printf("warning: %s", message)
	}
}

// ChartName is the chart of a chart artifact reference,
// registry/charts/<org>/<name>:<tag>.
func ChartName(reference string) string {
	name, _, _ := strings.Cut(reference[strings.LastIndex(reference, "/")+1:], ":")
	return name
}
