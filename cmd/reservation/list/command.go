package list

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	"k8s.io/client-go/dynamic"

	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

const (
	name             = "list"
	shortDescription = "List the active reservations on a management cluster."
	longDescription  = `List the active reservations on a management cluster.

The command reads the reservations ConfigMap (giantswarm/reservations) live
from the management cluster, through the kube context --context names
(teleport.giantswarm.io-<cluster> by default, which tsh kube login writes). It
needs no checkout of the GitOps repository and no GitHub token. It shows what
Flux applied, so a reservation pushed in the last few minutes can be missing.

For each active reservation it prints the app, the user, the branch, the pull
request, the scope and the expiry, exactly as recorded in the ConfigMap.`
	example = `  devctl reservation list --cluster graveler`
)

type Config struct {
	Logger micrologger.Logger
	Stderr io.Writer
	Stdout io.Writer
	// OpenCluster returns the client for a cluster's kube context. Empty is
	// rolloutwait.OpenCluster; tests pass a fake.
	OpenCluster func(cluster, kubeContext string) (dynamic.Interface, error)
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	if config.OpenCluster == nil {
		config.OpenCluster = func(cluster, kubeContext string) (dynamic.Interface, error) {
			return rolloutwait.OpenCluster(cluster, kubeContext, nil)
		}
	}

	f := &flag{}

	r := &runner{
		flag:        f,
		logger:      config.Logger,
		openCluster: config.OpenCluster,
		stderr:      config.Stderr,
		stdout:      config.Stdout,
	}

	c := &cobra.Command{
		Use:     name,
		Args:    cobra.NoArgs,
		Short:   shortDescription,
		Long:    longDescription,
		Example: example,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
