package kubeconfig

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"text/template"

	"github.com/giantswarm/k8sclient"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

const kubeconfigTemplate = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: {{.CA}}
    server: https://{{.APIUrl}}
  name: tenant-cluster
contexts:
- context:
    cluster: tenant-cluster
    user: ci-user
  name: tenant-cluster-context
current-context: tenant-cluster-context
kind: Config
preferences: {}
users:
- name: ci-user
  user:
    client-certificate-data: {{.Certificate}}
    client-key-data: {{.Key}}
`

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

	var apiUrl string
	if r.flag.Provider == "azure" {
		cluster, err := clients.G8sClient().ProviderV1alpha1().AzureConfigs("default").Get(r.flag.ClusterID, v1.GetOptions{})
		if err != nil {
			return microerror.Mask(err)
		}
		apiUrl = cluster.Spec.Cluster.Kubernetes.API.Domain
	} else if r.flag.Provider == "aws" {
		cluster, err := clients.G8sClient().ProviderV1alpha1().AWSConfigs("default").Get(r.flag.ClusterID, v1.GetOptions{})
		if err != nil {
			return microerror.Mask(err)
		}
		apiUrl = cluster.Spec.Cluster.Kubernetes.API.Domain
	} else if r.flag.Provider == "kvm" {
		cluster, err := clients.G8sClient().ProviderV1alpha1().KVMConfigs("default").Get(r.flag.ClusterID, v1.GetOptions{})
		if err != nil {
			return microerror.Mask(err)
		}
		apiUrl = cluster.Spec.Cluster.Kubernetes.API.Domain
	} else {
		return microerror.Mask(fmt.Errorf("invalid provider specified"))
	}

	secret, err := clients.K8sClient().CoreV1().Secrets("default").Get(getSecretName(r.flag.ClusterID), v1.GetOptions{})
	if err != nil {
		return microerror.Mask(err)
	}

	var kubeconfigFile *os.File
	if r.flag.Output == "" {
		kubeconfigFile = os.Stdout
	} else {
		kubeconfigFile, err = os.Create(r.flag.Output)
		if err != nil {
			return microerror.Mask(err)
		}
		defer kubeconfigFile.Close()
	}

	t := template.Must(template.New("kubeconfig").Parse(kubeconfigTemplate))
	err = t.Execute(kubeconfigFile, map[string]interface{}{
		"APIUrl":      apiUrl,
		"CA":          base64.StdEncoding.EncodeToString(secret.Data["ca"]),
		"Certificate": base64.StdEncoding.EncodeToString(secret.Data["crt"]),
		"ClusterID":   r.flag.ClusterID,
		"Key":         base64.StdEncoding.EncodeToString(secret.Data["key"]),
	})
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func getSecretName(tenantClusterId string) string {
	return fmt.Sprintf("%s-api", tenantClusterId)
}
