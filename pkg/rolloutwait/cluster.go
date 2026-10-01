package rolloutwait

import (
	"context"
	"fmt"
	"net/http"
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
)

// The resources a rollout wait reads.
var (
	helmReleases    = schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
	ociRepositories = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "ocirepositories"}
	helmCharts      = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "helmcharts"}
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
	}
}

// OpenCluster opens the kube context from the default kubeconfig loading
// rules (KUBECONFIG, ~/.kube/config) with the requests going through wrap.
// A context that does not exist is a usage error naming tsh kube login.
func OpenCluster(installation, kubeContext string, wrap func(http.RoundTripper) http.RoundTripper) (dynamic.Interface, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: kubeContext}).ClientConfig()
	if err != nil {
		return nil, agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "kube context %s: %v; `tsh kube login %s` writes it, --context names another", kubeContext, err, installation)
	}
	config.WrapTransport = wrap
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("kube context %s: %w", kubeContext, err)
	}
	return client, nil
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
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":%q}}}`, w.clock.Now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	_, err := w.client.Resource(o.resource).Namespace(o.namespace).Patch(ctx, o.name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return w.clusterErr("requesting a reconcile of "+o.String(), err)
	}
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
