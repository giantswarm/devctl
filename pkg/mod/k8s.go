package mod

import (
	"io/ioutil"
	"path"
	"strings"
)

func ReadKubernetesVersion(dir string) (string, error) {
	dat, err := ioutil.ReadFile(path.Join(dir, "k8s-version.txt"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(dat)), nil
}

func KnownKubernetesDependencies() []string {
	return []string{
		"k8s.io/api",
		"k8s.io/apiextensions-apiserver",
		"k8s.io/apimachinery",
		"k8s.io/apiserver",
		"k8s.io/cli-runtime",
		"k8s.io/client-go",
		"k8s.io/cloud-provider",
		"k8s.io/cluster-bootstrap",
		"k8s.io/code-generator",
		"k8s.io/component-base",
		"k8s.io/cri-api",
		"k8s.io/csi-translation-lib",
		"k8s.io/kube-aggregator",
		"k8s.io/kube-controller-manager",
		"k8s.io/kube-proxy",
		"k8s.io/kube-scheduler",
		"k8s.io/kubelet",
		"k8s.io/legacy-cloud-providers",
		"k8s.io/metrics",
		"k8s.io/sample-apiserver",
	}
}
