package reposetup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/giantswarm/microerror"
)

// Option is a choice a template's scaffold offers. The dry run lists the
// options of the derived template on [Entry.Options]; [RenderRequest.Options]
// selects among them by name. An option without Values is free text.
type Option struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Values      []string `json:"values,omitempty"`
	Default     string   `json:"default,omitempty"`
}

// The chart-only template's options: what `devctl app bootstrap` scaffolded
// by flag, offered per declaration instead.
const (
	// OptionSync is how the chart's templates follow an upstream chart.
	OptionSync = "sync"
	// OptionPatch is how local changes are applied on top of the synced
	// templates.
	OptionPatch = "patch"
	// OptionUpstreamRepo is the git URL of the upstream repository the
	// chart is synced from.
	OptionUpstreamRepo = "upstream-repo"
	// OptionUpstreamChart is the path of the chart inside the upstream
	// repository.
	OptionUpstreamChart = "upstream-chart"

	// SyncNone renders no sync configuration.
	SyncNone = "none"
	// SyncVendir renders vendir.yml, which vendors the upstream chart's
	// templates into helm/<name>/templates.
	SyncVendir = "vendir"
	// PatchNone renders no patch scaffolding.
	PatchNone = "none"
	// PatchScript renders sync/sync.sh, which runs the sync, applies
	// sync/patches/*/patch.sh and stores the resulting diffs.
	PatchScript = "script"
)

// chartOptions are the options of [TemplateChart].
var chartOptions = []Option{
	{
		Name:        OptionSync,
		Description: "how the chart's templates follow an upstream chart: vendir renders vendir.yml vendoring the upstream chart's templates into helm/<name>/templates (needs upstream-repo and upstream-chart)",
		Values:      []string{SyncNone, SyncVendir},
		Default:     SyncNone,
	},
	{
		Name:        OptionPatch,
		Description: "how local changes are applied on top of the synced templates: script renders sync/sync.sh, which syncs, runs sync/patches/*/patch.sh and stores the diffs (needs sync: vendir)",
		Values:      []string{PatchNone, PatchScript},
		Default:     PatchNone,
	},
	{
		Name:        OptionUpstreamRepo,
		Description: "git URL of the upstream repository the chart is synced from, e.g. https://github.com/some-org/some-repo; also credited in the README",
	},
	{
		Name:        OptionUpstreamChart,
		Description: "path of the chart inside the upstream repository, e.g. charts/some-chart",
	},
}

// templateOptions returns the options a template's scaffold offers.
func templateOptions(t Template) []Option {
	if t == TemplateChart {
		return chartOptions
	}
	return nil
}

// resolveOptions returns the chosen options with the defaults filled in,
// refusing a name the template does not offer or a value outside an
// option's values.
func resolveOptions(t Template, chosen map[string]string) (map[string]string, error) {
	offered := templateOptions(t)
	resolved := make(map[string]string, len(offered))
	for _, o := range offered {
		resolved[o.Name] = o.Default
	}

	names := make([]string, 0, len(chosen))
	for name := range chosen {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		value := chosen[name]
		var option *Option
		for i := range offered {
			if offered[i].Name == name {
				option = &offered[i]
			}
		}
		if option == nil {
			return nil, microerror.Maskf(invalidConfigError, "option %q: not offered by template %s", name, t)
		}
		if len(option.Values) > 0 && !contains(option.Values, value) {
			return nil, microerror.Maskf(invalidConfigError, "option %q: must be one of %s, got %q", name, strings.Join(option.Values, "|"), value)
		}
		resolved[name] = value
	}

	if resolved[OptionSync] == SyncVendir && (resolved[OptionUpstreamRepo] == "" || resolved[OptionUpstreamChart] == "") {
		return nil, microerror.Maskf(invalidConfigError, "option %q: %s needs %s and %s", OptionSync, SyncVendir, OptionUpstreamRepo, OptionUpstreamChart)
	}
	if resolved[OptionPatch] == PatchScript && resolved[OptionSync] != SyncVendir {
		return nil, microerror.Maskf(invalidConfigError, "option %q: %s needs %s %s", OptionPatch, PatchScript, OptionSync, SyncVendir)
	}

	return resolved, nil
}

// writeChartOptions renders the sync and patch scaffolding the options
// select. The first `vendir sync` is the author's: it needs the vendir
// binary and the upstream repository.
func writeChartOptions(dir, name string, options map[string]string) error {
	if options[OptionSync] == SyncVendir {
		vendir := fmt.Sprintf(vendirTemplate, options[OptionUpstreamRepo], options[OptionUpstreamChart], name, options[OptionUpstreamChart])
		if err := writeFile(filepath.Join(dir, "vendir.yml"), []byte(vendir), fileMode); err != nil {
			return microerror.Mask(err)
		}
	}

	if options[OptionPatch] == PatchScript {
		if err := os.MkdirAll(filepath.Join(dir, "sync", "patches"), dirMode); err != nil {
			return microerror.Mask(err)
		}
		if err := writeFile(filepath.Join(dir, "sync", "patches", ".gitkeep"), nil, fileMode); err != nil {
			return microerror.Mask(err)
		}
		script := strings.ReplaceAll(syncScriptTemplate, "{APP-NAME}", name)
		if err := writeFile(filepath.Join(dir, "sync", "sync.sh"), []byte(script), executableMode); err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// vendirTemplate is the vendir.yml `devctl app bootstrap` wrote: the
// upstream chart's templates vendored into the chart. ref main is a start;
// pin it to the upstream release the chart packages.
const vendirTemplate = `apiVersion: vendir.k14s.io/v1alpha1
kind: Config
minimumRequiredVersion: 0.12.0
directories:
- path: vendor
  contents:
  - path: .
    git:
      url: %s
      # Pin to the upstream release the chart packages, e.g. v1.17.2.
      ref: main
    includePaths:
    - %s/**/*
- path: helm/%s/templates
  contents:
  - path: .
    directory:
      path: vendor/%s/templates
`

// syncScriptTemplate is the sync/sync.sh `devctl app bootstrap` wrote.
const syncScriptTemplate = `#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
cd "${dir}/.."

# Stage 1 sync - intermediate to the ./vendor folder
set -x
vendir sync
helm dependency update helm/{APP-NAME}/
{ set +x; } 2>/dev/null

# Apply patches
for patch in sync/patches/*; do
    if [ -d "$patch" ] && [ -x "$patch/patch.sh" ]; then
        "$patch/patch.sh"
    fi
done

# Store diffs
mkdir -p ./diffs
rm -f ./diffs/*
for f in $(git --no-pager diff --no-exit-code --no-color --no-index vendor/{APP-NAME} helm --name-only) ; do
        [[ "$f" == "helm/{APP-NAME}/Chart.yaml" ]] && continue
        [[ "$f" == "helm/{APP-NAME}/Chart.lock" ]] && continue
        [[ "$f" == "helm/{APP-NAME}/README.md" ]] && continue
        [[ "$f" == "helm/{APP-NAME}/values.schema.json" ]] && continue
        [[ "$f" == "helm/{APP-NAME}/values.yaml" ]] && continue
        [[ "$f" =~ ^helm/{APP-NAME}/charts/.* ]] && continue

        base_file="vendor/{APP-NAME}/${f#"helm/"}"
        [[ ! -e $base_file ]] && base_file="/dev/null"

        set +e
        set -x
        git --no-pager diff --no-exit-code --no-color --no-index "$base_file" "${f}" \
                > "./diffs/${f//\//__}.patch"
        { set +x; } 2>/dev/null
        set -e
        ret=$?
        if [ $ret -ne 0 ] && [ $ret -ne 1 ] ; then
                exit $ret
        fi
done
`
