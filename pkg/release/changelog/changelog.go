package changelog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// Regex patterns used by all Giant Swarm components
const (
	// Indicates that the target version has started.
	// Expects a line like "## [1.0.0] 2020-07-14" with optional v prefix on the version and optional date.
	commonStartPattern = "(?m)^## \\[v?(?P<Version>\\d+\\.\\d+\\.\\d+-?.*)\\].*(?P<Date>\\d{4}-\\d{2}\\-\\d{2})?$"
	// Indicates that the links following the final release in a CHANGELOG have been encountered.
	// Expects a line lke "[Unreleased]: https://github.com/giantswarm/kvm-operator/compare/v3.12.0...HEAD".
	commonEndPattern = "(?m)^\\[.*\\]:.*$"
)

type ParseParams struct {
	Tag            string
	Changelog      string
	Start          string
	Intermediate   string
	End            string
	AutoDetect     bool
	FilterPatterns []string // Changelog entries matching these are excluded
}

// Parameters defining how to parse and extract release info about all known components
var KnownComponents = map[string]ParseParams{
	// CAPA Provider Specific
	"aws-ebs-csi-driver": {
		Tag:       "https://github.com/giantswarm/aws-ebs-csi-driver-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/aws-ebs-csi-driver-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"aws-ebs-csi-driver-servicemonitors": {
		Tag:       "https://github.com/giantswarm/aws-ebs-csi-driver-servicemonitors-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/aws-ebs-csi-driver-servicemonitors-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"aws-nth-bundle": {
		Tag:       "https://github.com/giantswarm/aws-nth-bundle/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/aws-nth-bundle/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"aws-pod-identity-webhook": {
		Tag:       "https://github.com/giantswarm/aws-pod-identity-webhook/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/aws-pod-identity-webhook/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cluster-aws": {
		Tag:            "https://github.com/giantswarm/cluster-aws/releases/tag/v{{.Version}}",
		Changelog:      "https://raw.githubusercontent.com/giantswarm/cluster-aws/v{{.Version}}/CHANGELOG.md",
		Start:          commonStartPattern,
		End:            commonEndPattern,
		FilterPatterns: []string{clusterChartUpdateFilter},
	},
	"cloud-provider-aws": {
		Tag:       "https://github.com/giantswarm/aws-cloud-controller-manager-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/aws-cloud-controller-manager-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"irsa-servicemonitors": {
		Tag:       "https://github.com/giantswarm/irsa-servicemonitors-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/irsa-servicemonitors-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},

	// EKS Provider Specific
	"cluster-eks": {
		Tag:            "https://github.com/giantswarm/cluster-eks/releases/tag/v{{.Version}}",
		Changelog:      "https://raw.githubusercontent.com/giantswarm/cluster-eks/v{{.Version}}/CHANGELOG.md",
		Start:          commonStartPattern,
		End:            commonEndPattern,
		FilterPatterns: []string{clusterChartUpdateFilter},
	},
	"karpenter": {
		Tag:       "https://github.com/giantswarm/karpenter-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/karpenter-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"karpenter-bundle": {
		Tag:       "https://github.com/giantswarm/karpenter-bundle/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/karpenter-bundle/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"karpenter-taint-remover": {
		Tag:       "https://github.com/giantswarm/capa-karpenter-taint-remover/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/capa-karpenter-taint-remover/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"karpenter-crossplane-resources": {
		Tag:       "https://github.com/giantswarm/karpenter-crossplane-resources/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/karpenter-crossplane-resources/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"karpenter-nodepools": {
		Tag:       "https://github.com/giantswarm/karpenter-nodepools/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/karpenter-nodepools/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},

	// CAPZ Provider Specific
	"cluster-azure": {
		Tag:            "https://github.com/giantswarm/cluster-azure/releases/tag/v{{.Version}}",
		Changelog:      "https://raw.githubusercontent.com/giantswarm/cluster-azure/v{{.Version}}/CHANGELOG.md",
		Start:          commonStartPattern,
		End:            commonEndPattern,
		FilterPatterns: []string{clusterChartUpdateFilter},
	},
	"azure-cloud-controller-manager": {
		Tag:       "https://github.com/giantswarm/azure-cloud-controller-manager-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/azure-cloud-controller-manager-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"azure-cloud-node-manager": {
		Tag:       "https://github.com/giantswarm/azure-cloud-node-manager-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/azure-cloud-node-manager-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"azuredisk-csi-driver": {
		Tag:       "https://github.com/giantswarm/azuredisk-csi-driver-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/azuredisk-csi-driver-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"azurefile-csi-driver": {
		Tag:       "https://github.com/giantswarm/azurefile-csi-driver-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/azurefile-csi-driver-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},

	// AKS Provider Specific
	"cluster-aks": {
		Tag:            "https://github.com/giantswarm/cluster-aks/releases/tag/v{{.Version}}",
		Changelog:      "https://raw.githubusercontent.com/giantswarm/cluster-aks/v{{.Version}}/CHANGELOG.md",
		Start:          commonStartPattern,
		End:            commonEndPattern,
		FilterPatterns: []string{clusterChartUpdateFilter},
	},

	// CAPV Provider Specific
	"cluster-vsphere": {
		Tag:            "https://github.com/giantswarm/cluster-vsphere/releases/tag/v{{.Version}}",
		Changelog:      "https://raw.githubusercontent.com/giantswarm/cluster-vsphere/v{{.Version}}/CHANGELOG.md",
		Start:          commonStartPattern,
		End:            commonEndPattern,
		FilterPatterns: []string{clusterChartUpdateFilter},
	},
	"cloud-provider-vsphere": {
		Tag:       "https://github.com/giantswarm/cloud-provider-vsphere-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cloud-provider-vsphere-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"kube-vip": {
		Tag:       "https://github.com/giantswarm/kube-vip-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/kube-vip-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"kube-vip-cloud-provider": {
		Tag:       "https://github.com/giantswarm/kube-vip-cloud-provider-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/kube-vip-cloud-provider-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"vsphere-csi-driver": {
		Tag:       "https://github.com/giantswarm/vsphere-csi-driver-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/vsphere-csi-driver-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},

	// CAPVCD Provider Specific
	"cluster-cloud-director": {
		Tag:            "https://github.com/giantswarm/cluster-cloud-director/releases/tag/v{{.Version}}",
		Changelog:      "https://raw.githubusercontent.com/giantswarm/cluster-cloud-director/v{{.Version}}/CHANGELOG.md",
		Start:          commonStartPattern,
		End:            commonEndPattern,
		FilterPatterns: []string{clusterChartUpdateFilter},
	},
	"cloud-provider-cloud-director": {
		Tag:       "https://github.com/giantswarm/cloud-provider-cloud-director-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cloud-provider-cloud-director-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},

	// Common Apps
	"capi-node-labeler": {
		Tag:       "https://github.com/giantswarm/capi-node-labeler-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/capi-node-labeler-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cert-exporter": {
		Tag:       "https://github.com/giantswarm/cert-exporter/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cert-exporter/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cert-manager": {
		Tag:       "https://github.com/giantswarm/cert-manager-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cert-manager-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cert-manager-crossplane-resources": {
		Tag:       "https://github.com/giantswarm/cert-manager-crossplane-resources/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cert-manager-crossplane-resources/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"chart-operator-extensions": {
		Tag:       "https://github.com/giantswarm/chart-operator-extensions/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/chart-operator-extensions/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cilium": {
		Tag:       "https://github.com/giantswarm/cilium-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cilium-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cilium-crossplane-resources": {
		Tag:       "https://github.com/giantswarm/cilium-crossplane-resources/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cilium-crossplane-resources/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cilium-servicemonitors": {
		Tag:       "https://github.com/giantswarm/cilium-servicemonitors-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cilium-servicemonitors-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cilium-prerequisites": {
		Tag:       "https://github.com/giantswarm/cilium-prerequisites/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cilium-prerequisites/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cluster": {
		Tag:       "https://github.com/giantswarm/cluster/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cluster/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cluster-autoscaler": {
		Tag:       "https://github.com/giantswarm/cluster-autoscaler-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cluster-autoscaler-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"cluster-autoscaler-crossplane-resources": {
		Tag:       "https://github.com/giantswarm/cluster-autoscaler-crossplane-resources/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/cluster-autoscaler-crossplane-resources/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"coredns": {
		Tag:       "https://github.com/giantswarm/coredns-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/coredns-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"coredns-extensions": {
		Tag:       "https://github.com/giantswarm/coredns-extensions-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/coredns-extensions-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"etcd-defrag": {
		Tag:       "https://github.com/giantswarm/etcd-defrag-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/etcd-defrag-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"etcd-k8s-res-count-exporter": {
		Tag:       "https://github.com/giantswarm/etcd-kubernetes-resources-count-exporter/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/etcd-kubernetes-resources-count-exporter/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"external-dns": {
		Tag:       "https://github.com/giantswarm/external-dns-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/external-dns-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"external-dns-crossplane-resources": {
		Tag:       "https://github.com/giantswarm/external-dns-crossplane-resources/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/external-dns-crossplane-resources/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"k8s-audit-metrics": {
		Tag:       "https://github.com/giantswarm/k8s-audit-metrics/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/k8s-audit-metrics/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"k8s-dns-node-cache": {
		Tag:       "https://github.com/giantswarm/k8s-dns-node-cache-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/k8s-dns-node-cache-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"metrics-server": {
		Tag:       "https://github.com/giantswarm/metrics-server-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/metrics-server-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"net-exporter": {
		Tag:       "https://github.com/giantswarm/net-exporter/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/net-exporter/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"network-policies": {
		Tag:       "https://github.com/giantswarm/network-policies-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/network-policies-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"node-exporter": {
		Tag:       "https://github.com/giantswarm/node-exporter-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/node-exporter-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"node-problem-detector": {
		Tag:       "https://github.com/giantswarm/node-problem-detector-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/node-problem-detector-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"observability-bundle": {
		Tag:       "https://github.com/giantswarm/observability-bundle/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/observability-bundle/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"observability-policies": {
		Tag:       "https://github.com/giantswarm/observability-policies-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/observability-policies-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"priority-classes": {
		Tag:       "https://github.com/giantswarm/priority-classes/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/priority-classes/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"prometheus-blackbox-exporter": {
		Tag:       "https://github.com/giantswarm/prometheus-blackbox-exporter-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/prometheus-blackbox-exporter-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"rbac-bootstrap": {
		Tag:       "https://github.com/giantswarm/rbac-bootstrap-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/rbac-bootstrap-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"security-bundle": {
		Tag:       "https://github.com/giantswarm/security-bundle/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/security-bundle/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"teleport-kube-agent": {
		Tag:       "https://github.com/giantswarm/teleport-kube-agent-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/teleport-kube-agent-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"vertical-pod-autoscaler": {
		Tag:       "https://github.com/giantswarm/vertical-pod-autoscaler-app/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/vertical-pod-autoscaler-app/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"vertical-pod-autoscaler-crd": {
		Tag:       "https://github.com/giantswarm/vertical-pod-autoscaler-crd/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/vertical-pod-autoscaler-crd/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},

	// Core Components
	"flatcar": {
		// Flatcar has no parseable changelog: the release notes only link to
		// the Flatcar release page (Tag), so no Changelog URL is fetched.
		Tag: "https://www.flatcar.org/releases/#release-{{.Version}}",
	},
	"kubernetes": {
		Tag:          "https://github.com/kubernetes/kubernetes/releases/tag/v{{.Version}}",
		Changelog:    "https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-{{.Major}}.{{.Minor}}.md#v{{.Version}}",
		Start:        "(?m)^# v?(?P<Version>\\d+\\.\\d+\\.\\d+)$",
		Intermediate: "(?m)^## Changes by Kind$",
		End:          "(?m)^# .*$",
		AutoDetect:   true,
	},
	"os-tooling": {
		Tag:       "https://github.com/giantswarm/capi-image-builder/releases/tag/v{{.Version}}",
		Changelog: "https://raw.githubusercontent.com/giantswarm/capi-image-builder/v{{.Version}}/CHANGELOG.md",
		Start:     commonStartPattern,
		End:       commonEndPattern,
	},
	"containerd": {
		// containerd keeps its release notes on the GitHub release rather than in a
		// CHANGELOG we could parse, so only the tag URL is used.
		Tag: "https://github.com/containerd/containerd/releases/tag/v{{.Version}}",
	},
}

// GetRepoName extracts the repository name for a given component from its tag URL.
func GetRepoName(componentName string) (string, string) {
	if params, ok := KnownComponents[componentName]; ok {
		// e.g. "https://github.com/giantswarm/cluster-autoscaler-app/releases/tag/v{{.Version}}"
		return repoFromURL(params.Tag)
	}
	return "", ""
}

var githubRepoRegex = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)

// repoFromURL returns the owner and name of the GitHub repository a URL
// points into.
func repoFromURL(u string) (string, string) {
	matches := githubRepoRegex.FindStringSubmatch(u)
	if len(matches) > 2 {
		return matches[1], matches[2]
	}
	return "", ""
}

// Data about a component passed into templates that depend on versions
type versionTemplateData struct {
	Version string
	Major   uint64
	Minor   uint64
}

// Data about a particular component version returned from parsing a changelog
type Version struct {
	Link    string
	Name    string
	Content string
}

type CategorizedChanges struct {
	Breaking   []string
	Added      []string
	Changed    []string
	Deprecated []string
	Removed    []string
	Fixed      []string
	Security   []string
}

var categoryRegex = regexp.MustCompile(`^###\s+(?:\W+\s+)?(\w+)`)

// ParseChangelog returns the changes of a component from endVersion
// (exclusive) to currentVersion. It reads them from the component's
// CHANGELOG.md at the tag, and from its GitHub Releases when that file has no
// section for currentVersion: a repository on the auto-release flow keeps its
// changelog on GitHub Releases, and its CHANGELOG.md only points at them.
func ParseChangelog(githubToken, componentName, currentVersion, endVersion string, extraFilterPatterns ...string) (*Version, error) {
	params, ok := KnownComponents[componentName]
	if !ok {
		return nil, microerror.Mask(fmt.Errorf("unknown component: %s", componentName))
	}

	if componentName == "containerd" {
		// containerd is derived from the image-builder release our os-tooling version pins,
		// and has no CHANGELOG in the format parsed below. Link to the release and stop.
		return &Version{
			Name:    currentVersion,
			Link:    strings.Replace(params.Tag, "{{.Version}}", currentVersion, 1),
			Content: "",
		}, nil
	}

	if componentName == "flatcar" {
		// Flatcar's "changelog" is a large JSON manifest we don't parse. Skip
		// fetching it entirely and just return a link to the release notes.
		// This avoids a wasted (and frequently slow/timing-out) network request.
		return &Version{
			Name:    currentVersion,
			Link:    strings.Replace(params.Tag, "{{.Version}}", currentVersion, 1),
			Content: "",
		}, nil
	}

	templateData := &versionTemplateData{}
	templateData.Version = currentVersion

	if componentName == kubernetesComponentName {
		semVer, err := semver.NewVersion(currentVersion)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		templateData.Major = semVer.Major()
		templateData.Minor = semVer.Minor()
	}

	// Build release link using the template from the params
	changelogURLTemplate, err := template.New("url").Parse(params.Changelog)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	var changelogURLBuilder strings.Builder
	err = changelogURLTemplate.Execute(&changelogURLBuilder, templateData)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	if componentName == kubernetesComponentName || componentName == "os-tooling" {
		// Kubernetes is not parsed, and os-tooling's repository is protected:
		// link to the changelog and stop.
		return &Version{
			Name:    currentVersion,
			Link:    changelogURLBuilder.String(),
			Content: "",
		}, nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(changelogURLBuilder.String())
	if err != nil {
		return nil, microerror.Mask(err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// A tag without CHANGELOG.md: its GitHub Releases are read below.
		body = nil
	default:
		return nil, microerror.Mask(fmt.Errorf("reading %s: %s", changelogURLBuilder.String(), response.Status))
	}

	if endVersion == "" {
		endVersion = currentVersion
	}

	compareRange := ""
	compareLink := ""
	if currentVersion == endVersion {
		// When there's no previous version or they're the same, link to single release
		compareRange = fmt.Sprintf("v%s", currentVersion)
		compareLink = strings.Replace(params.Tag, "{{.Version}}", currentVersion, 1)
	} else {
		// When there's a version range, use comparison link
		compareRange = fmt.Sprintf("v%s...v%s", endVersion, currentVersion)
		compareLink = fmt.Sprintf("%s/compare/%s", splitBaseURL(params.Tag), compareRange)
	}

	endSemVer, err := semver.NewVersion(endVersion)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	filterPatterns := append(append([]string{}, params.FilterPatterns...), extraFilterPatterns...)

	changes, found := parseSections(string(body), currentVersion, endVersion, endSemVer, filterPatterns)
	if !found {
		notes, err := releaseNotesAsChangelog(githubToken, params.Tag, currentVersion, endSemVer)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		changes, found = parseSections(notes, currentVersion, endVersion, endSemVer, filterPatterns)
	}

	// If we never actually parsed anything, raise the error
	if !found {
		return nil, microerror.Mask(fmt.Errorf("version range [%s] not found in changelog nor in GitHub releases", compareRange))
	}

	return &Version{
		Name:    compareRange,
		Link:    compareLink,
		Content: changes.render(),
	}, nil
}

// breakingMarker is how a git-cliff release note on the auto-release flow
// marks a breaking change in its group.
const breakingMarker = "[**breaking**]"

// parseSections collects the changes of the "## [<version>]" sections of a
// Keep-a-Changelog document from currentVersion down to, not including,
// endVersion. It reports whether the document has a section for
// currentVersion.
func parseSections(body, currentVersion, endVersion string, endSemVer *semver.Version, filterPatterns []string) (CategorizedChanges, bool) {
	categorizedChanges := CategorizedChanges{}
	inSection := false
	var currentCategory string

	startHeading := fmt.Sprintf("## [%s]", currentVersion)
	stopHeading := fmt.Sprintf("## [%s]", endVersion)

	for _, line := range strings.Split(body, "\n") {
		// Don't trim spaces yet - we need them to detect indentation
		originalLine := line
		line = strings.TrimSpace(line)

		// Parse between start and stop headings
		if strings.Contains(line, startHeading) {
			inSection = true
			continue
		}
		if inSection && strings.Contains(line, stopHeading) {
			break
		}

		if inSection && strings.HasPrefix(line, "## [") {
			// Extract version from heading, e.g., "## [1.2.3]"
			versionStr := strings.TrimPrefix(line, "## [")
			versionStr = strings.Split(versionStr, "]")[0]

			ver, err := semver.NewVersion(versionStr)
			if err == nil {
				// Stop if we've reached a version older than or equal to the end version
				if !ver.GreaterThan(endSemVer) {
					break
				}
			}

			// Continue parsing intermediate versions - don't stop until we hit the actual endVersion.
			// This allows collecting changes from all versions between currentVersion and endVersion.
			// For example, when parsing from v1.2.0 to v1.0.0, this will include changes from
			// v1.1.1, v1.1.0, etc., providing customers with a complete view of all changes.
			// Reset category when encountering intermediate version headers to ensure
			// proper categorization of changes from each version section.
			currentCategory = ""
			continue
		}

		if !inSection {
			continue
		}

		if matches := categoryRegex.FindStringSubmatch(line); len(matches) > 1 {
			currentCategory = matches[1]
			continue
		}

		// Use originalLine to preserve indentation for bullet point detection.
		// Detect sub-bullets: any line starting with 2+ spaces then "- " or "* "
		// (covers both 2-space and 4-space indentation conventions).
		trimmedLeft := strings.TrimLeft(originalLine, " \t")
		indent := len(originalLine) - len(trimmedLeft)
		if indent >= 2 && (strings.HasPrefix(trimmedLeft, "- ") || strings.HasPrefix(trimmedLeft, "* ")) {
			item := strings.TrimSpace(trimmedLeft[2:])
			// Append to the last item in the current category, avoiding duplicates
			if items := categorizedChanges.category(currentCategory); items != nil && len(*items) > 0 {
				last := &(*items)[len(*items)-1]
				subBullet := "\n  - " + item
				if !strings.Contains(*last, subBullet) {
					*last += subBullet
				}
			}
		} else if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			// Main bullet point
			item := strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* ")
			item = strings.TrimSpace(item)
			if matchesFilterPatterns(item, filterPatterns) {
				continue
			}
			category := currentCategory
			if category != "" && strings.Contains(item, breakingMarker) {
				category = "Breaking"
			}
			if items := categorizedChanges.category(category); items != nil {
				*items = appendUnique(*items, item)
			}
		}
	}

	return categorizedChanges, inSection
}

// category returns the list of changes a "### <name>" heading collects into,
// or nil for a heading no list takes.
func (c *CategorizedChanges) category(name string) *[]string {
	switch name {
	case "Breaking":
		return &c.Breaking
	case "Added":
		return &c.Added
	case "Changed":
		return &c.Changed
	case "Deprecated":
		return &c.Deprecated
	case "Removed":
		return &c.Removed
	case "Fixed":
		return &c.Fixed
	case "Security":
		return &c.Security
	}
	return nil
}

// render writes the changes as the consolidated changelog of a release note.
func (c CategorizedChanges) render() string {
	sections := []struct {
		heading string
		items   []string
	}{
		{"#### :warning: Breaking Changes", c.Breaking},
		{"#### Added", c.Added},
		{"#### Changed", c.Changed},
		{"#### Deprecated", c.Deprecated},
		{"#### Removed", c.Removed},
		{"#### Fixed", c.Fixed},
		{"#### Security", c.Security},
	}

	var sb strings.Builder
	for _, section := range sections {
		if len(section.items) == 0 {
			continue
		}
		sb.WriteString(section.heading + "\n\n")
		for _, item := range section.items {
			sb.WriteString("- " + item + "\n")
		}
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

// githubAPIURL is the GitHub API the GitHub Releases of a component are read
// from.
var githubAPIURL = "https://api.github.com/"

// releaseNotesAsChangelog reads the GitHub Releases of the component whose
// release page is tagURL and writes the notes of those after endVersion up to
// currentVersion as a Keep-a-Changelog document, newest first, one
// "## [<version>]" section each. currentVersion's own release is always in it,
// also when endVersion is currentVersion: a component new in a release has no
// previous version. A pre-release other than currentVersion is left out: its
// notes are cumulative, and the stable release after it carries them.
func releaseNotesAsChangelog(githubToken, tagURL, currentVersion string, endSemVer *semver.Version) (string, error) {
	owner, repo := repoFromURL(tagURL)
	if owner == "" {
		return "", microerror.Mask(fmt.Errorf("no GitHub repository in %s", tagURL))
	}
	current, err := semver.NewVersion(currentVersion)
	if err != nil {
		return "", microerror.Mask(err)
	}

	opts := []github.ClientOptionsFunc{github.WithURLs(&githubAPIURL, nil)}
	if githubToken != "" {
		opts = append(opts, github.WithAuthToken(githubToken))
	}
	client, err := github.NewClient(opts...)
	if err != nil {
		return "", microerror.Mask(err)
	}

	type note struct {
		version *semver.Version
		name    string
		body    string
	}
	var notes []note
	list := &github.ListOptions{PerPage: 100}
	for {
		releases, response, err := client.Repositories.ListReleases(context.Background(), owner, repo, list)
		if err != nil {
			return "", microerror.Mask(err)
		}
		for _, release := range releases {
			if release.GetDraft() {
				continue
			}
			version, err := semver.NewVersion(release.GetTagName())
			if err != nil {
				continue
			}
			if !version.Equal(current) && (!version.GreaterThan(endSemVer) || version.GreaterThan(current) || version.Prerelease() != "") {
				continue
			}
			notes = append(notes, note{version: version, name: strings.TrimPrefix(release.GetTagName(), "v"), body: release.GetBody()})
		}
		if response.NextPage == 0 {
			break
		}
		list.Page = response.NextPage
	}

	sort.Slice(notes, func(i, j int) bool { return notes[i].version.GreaterThan(notes[j].version) })

	var sb strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&sb, "## [%s]\n\n%s\n\n", n.name, n.body)
	}
	return sb.String(), nil
}

// appendUnique appends item to slice only if it doesn't already exist.
func appendUnique(slice []string, item string) []string {
	for _, existing := range slice {
		if existing == item {
			return slice
		}
	}
	return append(slice, item)
}

// matchesFilterPatterns returns true if the item matches any of the given filter patterns.
func matchesFilterPatterns(item string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(item, pattern) {
			return true
		}
	}
	return false
}

func splitBaseURL(fullURL string) string {
	suffix := "/releases/tag/v{{.Version}}"

	if strings.HasSuffix(fullURL, suffix) {
		return strings.TrimSuffix(fullURL, suffix)
	}

	// If the suffix is not found, return the original URL
	return fullURL
}
