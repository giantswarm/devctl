package list

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

var configMaps = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

type runner struct {
	flag        *flag
	logger      micrologger.Logger
	openCluster func(cluster, kubeContext string) (dynamic.Interface, error)
	stdout      io.Writer
	stderr      io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(context.Background(), cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// run reads the reservations ConfigMap live from the cluster: that is what
// Flux applied, so it never depends on a checkout on disk. No GitHub token.
func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	kubeContext := r.flag.Context
	if kubeContext == "" {
		kubeContext = rolloutwait.ContextPrefix + r.flag.Cluster
	}
	client, err := r.openCluster(r.flag.Cluster, kubeContext)
	var exitErr *agentcli.ExitError
	if errors.As(err, &exitErr) {
		// An ExitError prints nothing, as the agent-facing commands write
		// their own JSON; this one is for a person.
		return microerror.Maskf(invalidFlagError, "%s", exitErr.Reason)
	}
	if err != nil {
		return microerror.Mask(err)
	}

	configMap, err := client.Resource(configMaps).Namespace("giantswarm").Get(ctx, "reservations", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return microerror.Maskf(clusterNotEnabledError,
			"management cluster %q has no ConfigMap giantswarm/reservations, so it is not enabled for reservations (or Flux did not apply it yet)", r.flag.Cluster)
	}
	if err != nil {
		return microerror.Mask(err)
	}

	data, _, err := unstructured.NestedStringMap(configMap.Object, "data")
	if err != nil {
		return microerror.Mask(err)
	}
	all, err := reservation.ParseReservations(data)
	if err != nil {
		return microerror.Mask(err)
	}

	// List returns every entry on record, including one already past its
	// Until that the reaper has not swept yet: that one is not active, and
	// must not be printed as if it still held the app.
	now := time.Now()
	var reservations []reservation.Reservation
	for _, res := range all {
		if res.Until.After(now) {
			reservations = append(reservations, res)
		}
	}

	if len(reservations) == 0 {
		// A push lands on the cluster only once Flux applies it.
		_, _ = fmt.Fprintf(r.stdout, "No active reservations on %s. A reservation pushed in the last few minutes shows up once Flux applies it.\n", r.flag.Cluster)
		return nil
	}

	w := tabwriter.NewWriter(r.stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "APP\tUSER\tBRANCH\tPULL REQUEST\tSCOPE\tEXPIRES")
	for _, res := range reservations {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			res.App, res.User, res.Branch, res.PullRequest, res.Scope, res.Until.Format("2006-01-02 15:04 MST"))
	}

	return microerror.Mask(w.Flush())
}
