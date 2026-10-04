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
	// Charts are the names of the release's charts.
	Charts []string
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
	warned                             map[string]bool
}

// New validates config.
func New(config Config) (*Waiter, error) {
	if config.Client == nil {
		return nil, errors.New("rolloutwait: Client must not be nil")
	}
	if len(config.Charts) == 0 {
		return nil, errors.New("rolloutwait: Charts must not be empty")
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
		warned:           map[string]bool{},
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
// failed, 2 at the timeout, 3 when no deployment of the charts exists or
// none follows the version.
func (w *Waiter) Wait(ctx context.Context, result *Result) error {
	result.Version = w.version
	result.Charts = w.chartNames()
	deadline := w.clock.Now().Add(w.clock.Scaled(w.timeout))
	interval := IntervalFloor
	reconciled := false
	for {
		list, err := w.observe(ctx)
		if err != nil {
			return err
		}
		result.Deployments = list
		if missing := w.missingHelmReleases(list); len(missing) > 0 {
			return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "no HelmRelease %s on %s", strings.Join(missing, ", "), w.installation)
		}
		if len(list) == 0 {
			return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "no HelmRelease or App on %s deploys %s", w.installation, strings.Join(result.Charts, ", "))
		}
		if err := w.outcome(list); err != nil {
			return err
		}
		if pendingDeployments(list) == nil {
			w.progress.Printf("%s runs %s on %s", strings.Join(result.Charts, ", "), w.version, w.installation)
			return nil
		}
		if w.reconcile && !reconciled {
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

// observe reads every HelmRelease and App that deploys one of the charts.
func (w *Waiter) observe(ctx context.Context) ([]Deployment, error) {
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
			w.warnOnce(fmt.Sprintf("%s will not deploy %s: %s", d.id(), w.version, d.Message))
			excluded = append(excluded, d.id()+": "+d.Message)
		}
	}
	if len(excluded) == len(list) {
		return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "nothing on %s will deploy %s: %s", w.installation, w.version, strings.Join(excluded, "; "))
	}
	return nil
}

// follows says whether a deployment gets the version on its own.
func follows(d Deployment) bool {
	return d.State != StateNotFollowing && d.State != StateSuspended
}

func (w *Waiter) requestReconciles(ctx context.Context, list []Deployment) error {
	for _, d := range pendingDeployments(list) {
		if d.Kind != KindHelmRelease || atLeast(d.RunningVersion, w.version) {
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
	return agentcli.NewExitError(agentcli.ExitTimeout, agentcli.VerdictTimeout, "%s not rolled out on %s after %s: %s", w.version, w.installation, w.timeout, strings.Join(waiting, "; "))
}

func pendingDeployments(list []Deployment) []Deployment {
	var p []Deployment
	for _, d := range list {
		if follows(d) && d.State != StateRolledOut {
			p = append(p, d)
		}
	}
	return p
}

func (w *Waiter) chartNames() []string {
	names := make([]string, 0, len(w.charts))
	for c := range w.charts {
		names = append(names, c)
	}
	slices.Sort(names)
	return names
}

// warnOnce adds a warning the first time it is seen: every poll judges the
// same deployments again.
func (w *Waiter) warnOnce(message string) {
	if w.warned[message] {
		return
	}
	w.warned[message] = true
	w.warn(message)
}

// ChartName is the chart of a chart artifact reference,
// registry/charts/<org>/<name>:<tag>.
func ChartName(reference string) string {
	name, _, _ := strings.Cut(reference[strings.LastIndex(reference, "/")+1:], ":")
	return name
}
