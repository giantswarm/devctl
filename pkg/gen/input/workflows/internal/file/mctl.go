package file

import (
	"embed"
	"path/filepath"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

// MctlVersion is the gsoci.azurecr.io/giantswarm/mctl image tag the mctl
// workflows of a management-clusters repository run, unless the repository
// overrides it with the MCTL_IMAGE Actions variable. Pinned here so the
// workflows and the binary they drive move together.
//
// renovate: datasource=docker depName=gsoci.azurecr.io/giantswarm/mctl
const MctlVersion = "0.1.16"

// mctlWorkflows are the mctl workflows of a management-clusters repository
// (flavour fleet), by the name `mctl create cmc-repo` used to write them under
// before devctl generated them. Each renders from mctl_<name>.yaml.template,
// with dashes as underscores.
var mctlWorkflows = []string{
	"close-cascade",
	"config",
	"create-mc",
	"init",
	"merge-cascade",
	"merge-gate",
	"patch-init",
	"validate-config",
	"validate-manifest",
}

//go:generate go run ../../../update-template-sha.go mctl_close_cascade.yaml.template
//go:generate go run ../../../update-template-sha.go mctl_config.yaml.template
//go:generate go run ../../../update-template-sha.go mctl_create_mc.yaml.template
//go:generate go run ../../../update-template-sha.go mctl_init.yaml.template
//go:generate go run ../../../update-template-sha.go mctl_merge_cascade.yaml.template
//go:generate go run ../../../update-template-sha.go mctl_merge_gate.yaml.template
//go:generate go run ../../../update-template-sha.go mctl_patch_init.yaml.template
//go:generate go run ../../../update-template-sha.go mctl_validate_config.yaml.template
//go:generate go run ../../../update-template-sha.go mctl_validate_manifest.yaml.template
//go:embed mctl_*.yaml.template*
var mctlTemplateFiles embed.FS

// NewMctlInputs returns the mctl workflows, one zz_generated.mctl_<name>.yaml
// each.
func NewMctlInputs(p params.Params) []input.Input {
	inputs := make([]input.Input, 0, len(mctlWorkflows))
	for _, name := range mctlWorkflows {
		base := "mctl_" + strings.ReplaceAll(name, "-", "_")
		tmpl := base + ".yaml.template"

		body, err := mctlTemplateFiles.ReadFile(tmpl)
		if err != nil {
			// The names are fixed and the files embedded with them.
			panic(err)
		}

		inputs = append(inputs, input.Input{
			Path:         params.RegenerableFileName(p, base+".yaml"),
			TemplateBody: string(body),
			TemplateDelims: input.InputTemplateDelims{
				Left:  templateDelimLeft,
				Right: templateDelimRight,
			},
			TemplateData: map[string]interface{}{
				templateKeyHeader: params.Header("#", input.TemplateSHA(mctlTemplateFiles, tmpl)),
				"MctlVersion":     MctlVersion,
			},
		})
	}

	return inputs
}

// NewMctlLegacyDeletionInputs returns Inputs that delete what `mctl create
// cmc-repo` wrote before devctl generated the mctl workflows: each workflow
// under its unprefixed name, and op.env, which the config workflow now fetches
// from giantswarm/mctl at run time.
func NewMctlLegacyDeletionInputs(p params.Params) []input.Input {
	inputs := make([]input.Input, 0, len(mctlWorkflows)+1)
	for _, name := range mctlWorkflows {
		inputs = append(inputs, input.Input{
			Delete: true,
			Path:   filepath.Join(p.Dir, name+".yaml"),
		})
	}
	inputs = append(inputs, input.Input{
		Delete: true,
		Path:   filepath.Join(p.Dir, "op.env"),
	})

	return inputs
}
