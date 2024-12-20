package changelog

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
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

type parseParams struct {
	tag          string
	changelog    string
	start        string
	intermediate string
	end          string
}

// Parameters defining how to parse and extract release info about all known components
var knownComponentParseParams = map[string]parseParams{
	// CAPA Provider Specific
	"aws-ebs-csi-driver": {
		tag:       "https://github.com/giantswarm/aws-ebs-csi-driver-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/aws-ebs-csi-driver-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"aws-ebs-csi-driver-servicemonitors": {
		tag:       "https://github.com/giantswarm/aws-ebs-csi-driver-servicemonitors-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/aws-ebs-csi-driver-servicemonitors-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"aws-nth-bundle": {
		tag:       "https://github.com/giantswarm/aws-nth-bundle/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/aws-nth-bundle/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cluster-aws": {
		tag:       "https://github.com/giantswarm/cluster-aws/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cluster-aws/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cloud-provider-aws": {
		tag:       "https://github.com/giantswarm/aws-cloud-controller-manager-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/aws-cloud-controller-manager-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"irsa-servicemonitors": {
		tag:       "https://github.com/giantswarm/irsa-servicemonitors-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/irsa-servicemonitors-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},

	// EKS Provider Specific
	"cluster-eks": {
		tag:       "https://github.com/giantswarm/cluster-eks/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cluster-eks/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},

	// CAPZ Provider Specific
	"cluster-azure": {
		tag:       "https://github.com/giantswarm/cluster-azure/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cluster-azure/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"azure-cloud-controller-manager": {
		tag:       "https://github.com/giantswarm/azure-cloud-controller-manager-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/azure-cloud-controller-manager-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"azure-cloud-node-manager": {
		tag:       "https://github.com/giantswarm/azure-cloud-node-manager-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/azure-cloud-node-manager-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"azuredisk-csi-driver": {
		tag:       "https://github.com/giantswarm/azuredisk-csi-driver-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/azuredisk-csi-driver-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"azurefile-csi-driver": {
		tag:       "https://github.com/giantswarm/azurefile-csi-driver-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/azurefile-csi-driver-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},

	// CAPV Provider Specific
	"cluster-vsphere": {
		tag:       "https://github.com/giantswarm/cluster-vsphere/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cluster-vsphere/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cloud-provider-vsphere": {
		tag:       "https://github.com/giantswarm/cloud-provider-vsphere-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cloud-provider-vsphere-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},

	// CAPVCD Provider Specific
	"cluster-cloud-director": {
		tag:       "https://github.com/giantswarm/cluster-cloud-director/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cluster-cloud-director/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cloud-provider-cloud-director": {
		tag:       "https://github.com/giantswarm/cloud-provider-cloud-director-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cloud-provider-cloud-director-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},

	// Common Apps
	"capi-node-labeler": {
		tag:       "https://github.com/giantswarm/capi-node-labeler/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/capi-node-labeler/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cert-exporter": {
		tag:       "https://github.com/giantswarm/cert-exporter/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cert-exporter/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cert-manager": {
		tag:       "https://github.com/giantswarm/cert-manager-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cert-manager-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"chart-operator-extensions": {
		tag:       "https://github.com/giantswarm/chart-operator-extensions/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/chart-operator-extensions/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cilium": {
		tag:       "https://github.com/giantswarm/cilium-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cilium-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cilium-crossplane-resources": {
		tag:       "https://github.com/giantswarm/cilium-crossplane-resources/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cilium-crossplane-resources/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cilium-servicemonitors": {
		tag:       "https://github.com/giantswarm/cilium-servicemonitors-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cilium-servicemonitors-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cilium-prerequisites": {
		tag:       "https://github.com/giantswarm/cilium-prerequisites/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cilium-prerequisites/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cluster-autoscaler": {
		tag:       "https://github.com/giantswarm/cluster-autoscaler-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cluster-autoscaler-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"coredns": {
		tag:       "https://github.com/giantswarm/coredns-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/coredns-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"etcd-k8s-res-count-exporter": {
		tag:       "https://github.com/giantswarm/etcd-kubernetes-resources-count-exporter/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/etcd-kubernetes-resources-count-exporter/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"external-dns": {
		tag:       "https://github.com/giantswarm/external-dns-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/external-dns-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"k8s-audit-metrics": {
		tag:       "https://github.com/giantswarm/k8s-audit-metrics/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/k8s-audit-metrics/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"k8s-dns-node-cache": {
		tag:       "https://github.com/giantswarm/k8s-dns-node-cache-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/k8s-dns-node-cache-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"metrics-server": {
		tag:       "https://github.com/giantswarm/metrics-server-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/metrics-server-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"net-exporter": {
		tag:       "https://github.com/giantswarm/net-exporter/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/net-exporter/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"network-policies": {
		tag:       "https://github.com/giantswarm/network-policies-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/network-policies-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"node-exporter": {
		tag:       "https://github.com/giantswarm/node-exporter-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/node-exporter-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"observability-bundle": {
		tag:       "https://github.com/giantswarm/observability-bundle/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/observability-bundle/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"observability-policies": {
		tag:       "https://github.com/giantswarm/observability-policies-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/observability-policies-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"prometheus-blackbox-exporter": {
		tag:       "https://github.com/giantswarm/prometheus-blackbox-exporter-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/prometheus-blackbox-exporter-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"security-bundle": {
		tag:       "https://github.com/giantswarm/security-bundle/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/security-bundle/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"teleport-kube-agent": {
		tag:       "https://github.com/giantswarm/teleport-kube-agent-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/teleport-kube-agent-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"vertical-pod-autoscaler": {
		tag:       "https://github.com/giantswarm/vertical-pod-autoscaler-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/vertical-pod-autoscaler-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"vertical-pod-autoscaler-crd": {
		tag:       "https://github.com/giantswarm/vertical-pod-autoscaler-crd/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/vertical-pod-autoscaler-crd/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},

	// Core Components
	"flatcar": {
		tag:       "https://www.flatcar-linux.org/releases/#release-{{.Version}}",
		changelog: "https://www.flatcar.org/releases-json/releases-stable.json",
	},
	"kubernetes": {
		tag:          "https://github.com/kubernetes/kubernetes/releases/tag/v{{.Version}}",
		changelog:    "https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-{{.Major}}.{{.Minor}}.md#v{{.Version}}",
		start:        "(?m)^# v?(?P<Version>\\d+\\.\\d+\\.\\d+)$",
		intermediate: "(?m)^## Changes by Kind$",
		end:          "(?m)^# .*$",
	},
}

// Data about a component passed into templates that depend on versions
type versionTemplateData struct {
	Major   uint64
	Minor   uint64
	Version string
}

// Data about a particular component version returned from parsing a changelog
type Version struct {
	Link    string
	Name    string
	Content string
}

const kubernetes = "kubernetes"

func ParseChangelog(componentName, componentVersion string) (*Version, error) {
	params, ok := knownComponentParseParams[componentName]
	if !ok {
		return nil, microerror.Mask(errors.New("unknown component: " + componentName))
	}

	templateData := &versionTemplateData{}
	if componentName == kubernetes {
		templateData.Version = strings.Replace(componentVersion, ".", "", -1)
	} else {
		templateData.Version = componentVersion
	}

	parsedVersion, err := semver.NewVersion(componentVersion)
	if err == nil {
		templateData.Major = parsedVersion.Major()
		templateData.Minor = parsedVersion.Minor()
	}

	// Build release link using the template from the params
	var releaseLinkTemplate *template.Template
	if componentName == kubernetes {
		// Release link for Kubernetes is different use the changelog instead
		releaseLinkTemplate, err = template.New("link").Parse(params.changelog)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	} else {
		releaseLinkTemplate, err = template.New("link").Parse(params.tag)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}
	var releaseLinkBuffer strings.Builder
	err = releaseLinkTemplate.Execute(&releaseLinkBuffer, templateData)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	currentVersion := Version{
		Name: componentVersion,
		Link: releaseLinkBuffer.String(),
	}

	// Read full changelog and split into lines
	changelogURLTemplate, err := template.New("url").Parse(params.changelog)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	var changelogURLBuilder strings.Builder
	err = changelogURLTemplate.Execute(&changelogURLBuilder, templateData)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	response, err := http.Get(changelogURLBuilder.String())
	if err != nil {
		return nil, microerror.Mask(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	if componentName == "flatcar" || componentName == kubernetes {
		// Skip parsing and return the entire changelog for Flatcar and Kubernetes
		return &currentVersion, nil
	}

	// Regexes used for parsing lines below
	startPattern, err := regexp.Compile(params.start)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	intermediatePattern, err := regexp.Compile(params.intermediate)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	endPattern, err := regexp.Compile(params.end)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	versionFound := false
	reachedVersionContent := false
	reachedIntermediateMarker := params.intermediate == ""
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		// Skip blank lines
		if line == "" {
			continue
		}

		isStartLine := params.start != "" && startPattern.MatchString(line)
		isEndLine := params.end != "" && endPattern.MatchString(line)

		if (isStartLine || isEndLine) && reachedVersionContent {
			// The version has been fully extracted, stop parsing lines and return.
			break
		}

		if isStartLine {
			// Get "Version" part from regex
			subMatches := startPattern.FindStringSubmatch(line)
			var version string
			versionInStartPattern := false
			for i, subName := range startPattern.SubexpNames() {
				if subName == "Version" {
					versionInStartPattern = true
					version = subMatches[i]
				}
			}

			// Skip if this isn't the desired version
			if versionInStartPattern && version != componentVersion {
				continue
			}

			reachedVersionContent = true
			versionFound = true
		} else if reachedVersionContent {
			// An intermediate marker indicates that the non-useful info at the start
			// of the changelog has been passed.
			if !reachedIntermediateMarker {
				reachedIntermediateMarker = intermediatePattern.MatchString(line)
				continue
			}

			// Transform level 1-3 headers like "#"-"###" into at least level 4 headers like "####"
			for strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#### ") {
				line = "#" + line
			}

			currentVersion.Content += line + "\n"
		}
	}

	if !versionFound {
		currentVersion.Content = "Not found"
	}

	return &currentVersion, nil
}
