package rolloutwait

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// ContextPrefix names the kube context of an installation's management
// cluster that tsh kube login writes: the prefix and the installation.
const ContextPrefix = "teleport.giantswarm.io-"

// The kinds of Flux source a HelmRelease's chart comes from, and the group
// of the workloads.
const (
	kindOCIRepository = "OCIRepository"
	kindHelmChart     = "HelmChart"
	groupApps         = "apps"
	groupSource       = "source.toolkit.fluxcd.io"
)

// The resources a rollout wait reads.
var (
	helmReleases    = schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
	ociRepositories = schema.GroupVersionResource{Group: groupSource, Version: "v1", Resource: "ocirepositories"}
	helmCharts      = schema.GroupVersionResource{Group: groupSource, Version: "v1", Resource: "helmcharts"}
	apps            = schema.GroupVersionResource{Group: "application.giantswarm.io", Version: "v1alpha1", Resource: "apps"}
	deployments     = schema.GroupVersionResource{Group: groupApps, Version: "v1", Resource: "deployments"}
	statefulSets    = schema.GroupVersionResource{Group: groupApps, Version: "v1", Resource: "statefulsets"}
	daemonSets      = schema.GroupVersionResource{Group: groupApps, Version: "v1", Resource: "daemonsets"}
)

// ListKinds are the list kinds of the resources a rollout wait reads, what a
// fake dynamic client needs to serve them.
func ListKinds() map[schema.GroupVersionResource]string {
	return map[schema.GroupVersionResource]string{
		helmReleases:    "HelmReleaseList",
		ociRepositories: "OCIRepositoryList",
		helmCharts:      "HelmChartList",
		apps:            "AppList",
		deployments:     "DeploymentList",
		statefulSets:    "StatefulSetList",
		daemonSets:      "DaemonSetList",
		gitRepositories: "GitRepositoryList",
		kustomizations:  "KustomizationList",
		konfigurations:  "KonfigurationList",
		configMaps:      "ConfigMapList",
	}
}

// ClusterConfig names the management cluster OpenCluster opens.
type ClusterConfig struct {
	Installation string
	// Context is the kube context, ContextPrefix+Installation by default.
	Context string
	// Kubeconfig is the one file the context is read from; empty is the
	// default loading rules (KUBECONFIG, ~/.kube/config).
	Kubeconfig string
	// Wrap wraps the transport of every request.
	Wrap func(http.RoundTripper) http.RoundTripper
	// Teleport logs in when the kubeconfig lacks the Teleport context; nil
	// never logs in.
	Teleport Teleport
	// Progress is told about a login.
	Progress *agentcli.Progress
}

// OpenCluster opens the kube context of c with the requests going through
// c.Wrap. It reads the kubeconfig only, the cluster is not contacted.
//
// When the kubeconfig lacks the Teleport context (ContextPrefix +
// installation) and tsh is logged in, tsh kube login writes the context into
// a private temporary kubeconfig, which is read and removed again: neither
// the default kubeconfig nor --kubeconfig is written. A tsh that is not
// logged in is exit 8. Any other context that does not exist, or one tsh is
// not installed to write, is a usage error naming the contexts that mention
// the installation, --context and tsh kube login.
func OpenCluster(ctx context.Context, c ClusterConfig) (dynamic.Interface, error) {
	rules := loadingRules(c.Kubeconfig)
	raw, err := rules.Load()
	if err != nil {
		return nil, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "loading the kubeconfig: %v", err)
	}
	if _, ok := raw.Contexts[c.Context]; !ok {
		missing := MissingContextError(c.Installation, c.Context, slices.Collect(maps.Keys(raw.Contexts)))
		if c.Teleport == nil || c.Context != ContextPrefix+c.Installation {
			return nil, missing
		}
		return openWithTeleport(ctx, c, missing)
	}
	return openContext(rules, c)
}

// openWithTeleport logs in to the installation into a private temporary
// kubeconfig and opens the context from it; the file is gone on return, the
// client keeps what it needs in memory (tsh's credential plugin runs per
// request without the file).
func openWithTeleport(ctx context.Context, c ClusterConfig, missing error) (dynamic.Interface, error) {
	loggedIn, why, err := c.Teleport.LoggedIn(ctx)
	if errors.Is(err, ErrNoTsh) {
		return nil, missing
	}
	if err != nil {
		return nil, fmt.Errorf("kube context %s does not exist, and %w", c.Context, err)
	}
	if !loggedIn {
		return nil, agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired, "kube context %s does not exist and tsh is not logged in (%s): sign in with `tsh login`, the wait then logs in to %s into a private kubeconfig; --context <name> reads the installation through another context", c.Context, why, c.Installation)
	}

	dir, err := os.MkdirTemp("", "devctl-rollout-wait-")
	if err != nil {
		return nil, fmt.Errorf("creating the private kubeconfig: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	private := filepath.Join(dir, "config")

	c.Progress.Printf("kube context %s is not in the kubeconfig; tsh kube login %s into a private kubeconfig", c.Context, c.Installation)
	if err := c.Teleport.KubeLogin(ctx, c.Installation, c.Context, private); err != nil {
		return nil, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "kube context %s does not exist and %v; --context <name> reads the installation through another context", c.Context, err)
	}
	rules := loadingRules(private)
	raw, err := rules.Load()
	if err != nil {
		return nil, fmt.Errorf("loading the kubeconfig tsh kube login %s wrote: %w", c.Installation, err)
	}
	if _, ok := raw.Contexts[c.Context]; !ok {
		return nil, fmt.Errorf("tsh kube login %s wrote no context %s", c.Installation, c.Context)
	}
	return openContext(rules, c)
}

// loadingRules reads the one file kubeconfig names, or the default rules.
func loadingRules(kubeconfig string) *clientcmd.ClientConfigLoadingRules {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfig
	return rules
}

// openContext builds the client of c.Context from rules.
func openContext(rules *clientcmd.ClientConfigLoadingRules, c ClusterConfig) (dynamic.Interface, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: c.Context}).ClientConfig()
	if err != nil {
		return nil, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "kube context %s: %v; `tsh kube login %s` writes it, --context names another", c.Context, err, c.Installation)
	}
	config.WrapTransport = c.Wrap
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("kube context %s: %w", c.Context, err)
	}
	return client, nil
}

// MissingContextError is the usage error for a kube context the kubeconfig
// does not have. An installation reached through another context than the
// one tsh kube login writes (kubectl gs login's gs-<installation>, a lab's
// own) is named with --context; the kubeconfig's contexts that mention the
// installation are listed as the candidates.
func MissingContextError(installation, kubeContext string, contexts []string) error {
	var candidates []string
	for _, c := range contexts {
		if strings.Contains(c, installation) {
			candidates = append(candidates, c)
		}
	}
	slices.Sort(candidates)
	hint := "no context in the kubeconfig mentions " + installation
	if len(candidates) > 0 {
		hint = "contexts in the kubeconfig that mention " + installation + ": " + strings.Join(candidates, ", ")
	}
	return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "kube context %s does not exist (%s); --context <name> reads the installation through another context, `tsh kube login %s` writes this one", kubeContext, hint, installation)
}

// object is a namespaced object of one resource.
type object struct {
	resource        schema.GroupVersionResource
	kind            string
	namespace, name string
}

func (o object) String() string { return o.kind + " " + o.namespace + "/" + o.name }

// list lists a resource in namespace (all namespaces when empty). A resource
// the cluster does not serve (the App CRD on a cluster without app-operator)
// lists as empty.
func (w *Waiter) list(ctx context.Context, resource schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error) {
	list, err := w.client.Resource(resource).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, w.clusterErr("listing "+resource.Resource, err)
	}
	return list.Items, nil
}

// requestReconcile sets Flux's reconcile.fluxcd.io/requestedAt on o, which
// makes its controller reconcile it now instead of at its next interval.
func (w *Waiter) requestReconcile(ctx context.Context, o object) error {
	requestedAt := w.clock.Now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":%q}}}`, requestedAt)
	_, err := w.client.Resource(o.resource).Namespace(o.namespace).Patch(ctx, o.name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return w.clusterErr("requesting a reconcile of "+o.String(), err)
	}
	w.requested[o.String()] = requestedAt
	return nil
}

// clusterErr places a failed request in the exit-code table: a context
// that is not signed in is exit 8, anything else a tooling failure.
func (w *Waiter) clusterErr(what string, err error) error {
	if apierrors.IsUnauthorized(err) || strings.Contains(err.Error(), "getting credentials") {
		return agentcli.NewExitError(agentcli.ExitAuthRequired, agentcli.VerdictAuthRequired, "%s on %s: %v; sign in with `tsh kube login %s`", what, w.kubeContext, err, w.installation)
	}
	return fmt.Errorf("%s on %s: %w", what, w.kubeContext, err)
}
