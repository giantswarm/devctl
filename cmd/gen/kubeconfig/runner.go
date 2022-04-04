package kubeconfig

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/giantswarm/k8sclient/v7/pkg/k8sclient"
	gskubeconfig "github.com/giantswarm/kubeconfig/v4"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	var err error

	clientsConfig := k8sclient.ClientsConfig{
		Logger:         r.logger,
		KubeConfigPath: r.flag.Kubeconfig,
	}
	clients, err := k8sclient.NewClients(clientsConfig)
	if err != nil {
		return microerror.Mask(err)
	}

	secret, err := clients.K8sClient().CoreV1().Secrets("default").Get(ctx, getSecretName(r.flag.ClusterID), metav1.GetOptions{})
	if err != nil {
		return microerror.Mask(err)
	}

	restConfig := clients.RESTConfig()
	restConfig.CertData = secret.Data["crt"]
	restConfig.CAData = secret.Data["ca"]
	restConfig.KeyData = secret.Data["key"]
	restConfig.Host = strings.Replace(restConfig.Host, "g8s", fmt.Sprintf("api.%s.k8s", r.flag.ClusterID), 1)

	kubeconfigContents, err := gskubeconfig.NewKubeConfigForRESTConfig(ctx, restConfig, r.flag.ClusterID, "default")
	if err != nil {
		return microerror.Mask(err)
	}

	if r.flag.Output == "" {
		fmt.Printf("%s", kubeconfigContents)
	} else {
		err = ioutil.WriteFile(r.flag.Output, kubeconfigContents, 0644) //nolint:gosec
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func getSecretName(tenantClusterId string) string {
	return fmt.Sprintf("%s-api", tenantClusterId)
}
