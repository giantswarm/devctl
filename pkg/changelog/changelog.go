package changelog

import (
	"errors"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
)

// Regex patterns used by all Giant Swarm components
const (
	commonStartPattern = "(?m)^## \\[v?(?P<Version>\\d+\\.\\d+\\.\\d+)\\].*(?P<Date>\\d{4}-\\d{2}\\-\\d{2})$"
	commonEndPattern   = "(?m)^\\[.*\\]:.*$"
)

// Parameters defining how to parse and extract release info about all known components
var knownComponentParseParams = map[string]parseParams{
	"app-operator": {
		tag:       "https://github.com/giantswarm/app-operator/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/app-operator/master/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"aws-operator": {
		tag:       "https://github.com/giantswarm/aws-operator/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/aws-operator/master/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cert-operator": {
		tag:       "https://github.com/giantswarm/cert-operator/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cert-operator/master/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"cluster-operator": {
		tag:       "https://github.com/giantswarm/cluster-operator/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cluster-operator/master/CHANGELOG.md",
		start:     commonStartPattern,
		end:       commonEndPattern,
	},
	"kubernetes": {
		tag:          "https://github.com/kubernetes/kubernetes/releases/tag/v{{.Version}}",
		changelog:    "https://raw.githubusercontent.com/kubernetes/kubernetes/master/CHANGELOG/CHANGELOG-{{.Major}}.{{.Minor}}.md",
		start:        "(?m)^# v?(?P<Version>\\d+\\.\\d+\\.\\d+)$",
		intermediate: "(?m)^## Changes by Kind$",
		end:          "(?m)^# .*$",
	},
	"etcd": {
		tag:       "https://github.com/etcd-io/etcd/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/etcd-io/etcd/master/CHANGELOG-{{.Major}}.{{.Minor}}.md",
		start:     "(?m)^## \\[v?(?P<Version>\\d+\\.\\d+\\.\\d+)\\].*$",
		end:       "(?m)^## .*$",
	},
	"aws-cni": {
		tag:       "https://github.com/aws/amazon-vpc-cni-k8s/releases/tag/v{{.Version}}",
		changelog: "https://raw.githubusercontent.com/aws/amazon-vpc-cni-k8s/master/CHANGELOG.md",
		start:     "(?m)^## v?(?P<Version>\\d+\\.\\d+\\.\\d+)$",
		end:       "(?m)^##? .*$",
	},
}

type parseParams struct {
	tag          string
	changelog    string
	start        string
	intermediate string
	end          string
}

type containerLinuxSoftware struct {
	Docker   []string `json:"docker"`
	Ignition []string `json:"ignition"`
	Kernel   []string `json:"kernel"`
	Rkt      []string `json:"rkt"`
	Systemd  []string `json:"systemd"`
}

type containerLinuxRelease struct {
	Channel       string                 `json:"channel"`
	Architectures []string               `json:"architectures"`
	ReleaseDate   string                 `json:"release_date"`
	MajorSoftware containerLinuxSoftware `json:"major_software"`
	ReleaseNotes  string                 `json:"release_notes"`
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

func ParseChangelog(componentName, componentVersion string) (*Version, error) {
	params, ok := knownComponentParseParams[componentName]
	if !ok {
		return nil, microerror.Mask(errors.New("unknown component: " + componentName))
	}

	templateData := versionTemplateData{
		Version: componentVersion,
	}
	parsedVersion, err := semver.NewVersion(componentVersion)
	if err == nil {
		templateData.Major = parsedVersion.Major()
		templateData.Minor = parsedVersion.Minor()
	}

	// Read full changelog and split into lines
	changelogURLTemplate, err := template.New("url").Parse(params.changelog)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	var changelogURLBuilder strings.Builder
	err = changelogURLTemplate.Execute(&changelogURLBuilder, templateData)
	response, err := http.Get(changelogURLBuilder.String())
	if err != nil {
		return nil, microerror.Mask(err)
	}
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	lines := strings.Split(string(body), "\n")

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

	var currentVersion Version
	reachedVersionContent := false
	reachedIntermediateMarker := params.intermediate == ""
	for _, line := range lines {
		// Skip blank lines
		if line == "" {
			continue
		}

		isStartLine := startPattern.MatchString(line)
		isEndLine := endPattern.MatchString(line)

		if (isStartLine || isEndLine) && reachedVersionContent {
			// The version has been fully extracted, stop parsing lines and return.
			break
		}

		if isStartLine {
			// Get "Version" part from regex
			subMatches := startPattern.FindStringSubmatch(line)
			var version string
			for i, subName := range startPattern.SubexpNames() {
				if subName == "Version" {
					version = subMatches[i]
				}
			}

			// Skip if this isn't the desired version
			if version != componentVersion {
				continue
			} else {
				reachedVersionContent = true
			}

			// Build release link using the template from the params
			releaseLinkTemplate, err := template.New("link").Parse(params.tag)
			if err != nil {
				return nil, microerror.Mask(err)
			}
			var releaseLinkBuffer strings.Builder
			err = releaseLinkTemplate.Execute(&releaseLinkBuffer, templateData)
			if err != nil {
				return nil, microerror.Mask(err)
			}

			// Actually initialize the version
			currentVersion = Version{
				Name: version,
				Link: releaseLinkBuffer.String(),
			}
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

	return &currentVersion, nil
}
