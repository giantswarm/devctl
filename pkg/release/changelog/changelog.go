package changelog

import (
	"fmt"
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
	"aws-pod-identity-webhook": {
		tag:       "https://github.com/giantswarm/aws-pod-identity-webhook/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/aws-pod-identity-webhook/v{{.Version}}/CHANGELOG.md",
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
	"karpenter-bundle": {
		tag:       "https://github.com/giantswarm/karpenter-bundle/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/karpenter-bundle/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"karpenter-nodepools": {
		tag:       "https://github.com/giantswarm/karpenter-nodepools/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/karpenter-nodepools/v{{.Version}}/CHANGELOG.md",
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
		tag:       "https://github.com/giantswarm/capi-node-labeler-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/capi-node-labeler-app/v{{.Version}}/CHANGELOG.md",
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
	"cert-manager-crossplane-resources": {
		tag:       "https://github.com/giantswarm/cert-manager-crossplane-resources/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cert-manager-crossplane-resources/v{{.Version}}/CHANGELOG.md",
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
	"coredns-extensions": {
		tag:       "https://github.com/giantswarm/coredns-extensions-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/coredns-extensions-app/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"etcd-defrag": {
		tag:       "https://github.com/giantswarm/etcd-defrag-app/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/etcd-defrag-app/v{{.Version}}/CHANGELOG.md",
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
	"os-tooling": {
		tag:       "https://github.com/giantswarm/capi-image-builder/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/capi-image-builder/v{{.Version}}/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
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
	Added   []string
	Changed []string
	Fixed   []string
	Removed []string
	// Add more categories if needed
}

var categoryRegex = regexp.MustCompile(`^### (\w+)`)

func ParseChangelog(componentName, currentVersion, endVersion string) (*Version, error) {
	params, ok := knownComponentParseParams[componentName]
	if !ok {
		return nil, microerror.Mask(fmt.Errorf("unknown component: %s", componentName))
	}

	templateData := &versionTemplateData{}
	templateData.Version = currentVersion

	if componentName == "kubernetes" {
		semVer, err := semver.NewVersion(currentVersion)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		templateData.Major = semVer.Major()
		templateData.Minor = semVer.Minor()
	}

	// Build release link using the template from the params
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
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	if componentName == "flatcar" {
		// Skip parsing Flatcar
		return &Version{
			Name:    currentVersion,
			Link:    strings.Replace(params.tag, "{{.Version}}", currentVersion, 1),
			Content: "",
		}, nil
	}

	if componentName == "kubernetes" {
		// Skip parsing Kubernetes
		return &Version{
			Name:    currentVersion,
			Link:    changelogURLBuilder.String(),
			Content: "",
		}, nil
	}

	if componentName == "os-tooling" {
		// Skip parsing os-tooling
		// protected repo
		return &Version{
			Name:    currentVersion,
			Link:    changelogURLBuilder.String(),
			Content: "",
		}, nil
	}

	// Split changelog into lines
	lines := strings.Split(string(body), "\n")

	inSection := false
	compareRange := ""
	compareLink := ""

	if endVersion == "" || currentVersion == endVersion {
		// When there's no previous version or they're the same, link to single release
		compareRange = fmt.Sprintf("v%s", currentVersion)
		compareLink = fmt.Sprintf("https://github.com/giantswarm/%s/releases/tag/v%s", componentName, currentVersion)
	} else {
		// When there's a version range, use comparison link
		compareRange = fmt.Sprintf("v%s...v%s", endVersion, currentVersion)
		compareLink = fmt.Sprintf("%s/compare/%s", splitBaseURL(params.tag), compareRange)
	}

	categorizedChanges := CategorizedChanges{}

	var currentCategory string

	startHeading := fmt.Sprintf("## [%s]", currentVersion)
	stopHeading := fmt.Sprintf("## [%s]", endVersion)

	for _, line := range lines {
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
		// // Continue parsing intermediate versions - don't stop until we hit the actual endVersion.
		// This allows collecting changes from all versions between currentVersion and endVersion.
		// For example, when parsing from v1.2.0 to v1.0.0, this will include changes from
		// v1.1.1, v1.1.0, etc., providing customers with a complete view of all changes.
		if inSection && strings.HasPrefix(line, "## [") && !strings.Contains(line, stopHeading) {
			// Reset category when encountering intermediate version headers to ensure
			// proper categorization of changes from each version section.
			currentCategory = ""
			continue
		}

		if inSection {
			if matches := categoryRegex.FindStringSubmatch(line); len(matches) > 1 {
				currentCategory = matches[1]
				continue
			}

			// Use originalLine to preserve indentation for bullet point detection
			if strings.HasPrefix(originalLine, "  - ") || strings.HasPrefix(originalLine, "  * ") {
				// Sub bullet point (indented)
				item := strings.TrimPrefix(strings.TrimPrefix(originalLine, "  - "), "  * ")
				item = strings.TrimSpace(item)
				// Append to the last item in the current category with proper indentation
				switch currentCategory {
				case "Added":
					if len(categorizedChanges.Added) > 0 {
						categorizedChanges.Added[len(categorizedChanges.Added)-1] += "\n  - " + item
					}
				case "Changed":
					if len(categorizedChanges.Changed) > 0 {
						categorizedChanges.Changed[len(categorizedChanges.Changed)-1] += "\n  - " + item
					}
				case "Fixed":
					if len(categorizedChanges.Fixed) > 0 {
						categorizedChanges.Fixed[len(categorizedChanges.Fixed)-1] += "\n  - " + item
					}
				case "Removed":
					if len(categorizedChanges.Removed) > 0 {
						categorizedChanges.Removed[len(categorizedChanges.Removed)-1] += "\n  - " + item
					}
				}
			} else if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
				// Main bullet point
				item := strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* ")
				item = strings.TrimSpace(item)
				switch currentCategory {
				case "Added":
					categorizedChanges.Added = append(categorizedChanges.Added, item)
				case "Changed":
					categorizedChanges.Changed = append(categorizedChanges.Changed, item)
				case "Fixed":
					categorizedChanges.Fixed = append(categorizedChanges.Fixed, item)
				case "Removed":
					categorizedChanges.Removed = append(categorizedChanges.Removed, item)
				}
			}
		}
	}

	// If we never actually parsed anything, raise the error
	if !inSection {
		return nil, microerror.Mask(fmt.Errorf("version range [%s] not found in changelog", compareRange))
	}

	// Build the consolidated changelog
	var sb strings.Builder

	if len(categorizedChanges.Added) > 0 {
		sb.WriteString("#### Added\n\n")
		for _, item := range categorizedChanges.Added {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")
	}

	if len(categorizedChanges.Changed) > 0 {
		sb.WriteString("#### Changed\n\n")
		for _, item := range categorizedChanges.Changed {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")
	}

	if len(categorizedChanges.Fixed) > 0 {
		sb.WriteString("#### Fixed\n\n")
		for _, item := range categorizedChanges.Fixed {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")
	}

	if len(categorizedChanges.Removed) > 0 {
		sb.WriteString("#### Removed\n\n")
		for _, item := range categorizedChanges.Removed {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")
	}

	consolidatedContent := sb.String()

	currentVersionStruct := Version{
		Name:    compareRange,
		Link:    compareLink,
		Content: consolidatedContent,
	}

	return &currentVersionStruct, nil
}

func splitBaseURL(fullURL string) string {
	suffix := "/releases/tag/v{{.Version}}"

	if strings.HasSuffix(fullURL, suffix) {
		return strings.TrimSuffix(fullURL, suffix)
	}

	// If the suffix is not found, return the original URL
	return fullURL
}
