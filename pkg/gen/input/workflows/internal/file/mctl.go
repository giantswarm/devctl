package file

import (
	"embed"
	"path/filepath"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

// MctlVersion is the mctl image tag the workflows run unless vars.MCTL_IMAGE overrides it.
// renovate: datasource=docker depName=gsoci.azurecr.io/giantswarm/mctl
const MctlVersion = "0.2.0"

// mctlWorkflows are the legacy file names; each renders from mctl_<name_with_underscores>.yaml.template.
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

// NewMctlInputs returns one zz_generated.mctl_<name>.yaml per mctl workflow.
func NewMctlInputs(p params.Params) []input.Input {
	inputs := make([]input.Input, 0, len(mctlWorkflows))
	for _, name := range mctlWorkflows {
		base := "mctl_" + strings.ReplaceAll(name, "-", "_")
		tmpl := base + ".yaml.template"

		body, err := mctlTemplateFiles.ReadFile(tmpl)
		if err != nil {
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

// NewMctlLegacyDeletionInputs deletes the unprefixed workflows and op.env written by `mctl create cmc-repo`.
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
