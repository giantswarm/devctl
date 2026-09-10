package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed helm-docs-regen.yaml.template
var helmDocsRegenTemplate string

//go:generate go run ../../../update-template-sha.go helm-docs-regen.yaml.template
//go:embed helm-docs-regen.yaml.template.sha
var helmDocsRegenTemplateSha string

// NewHelmDocsRegenInput generates the workflow that regenerates the chart
// README (helm-docs) and values.schema.json (helm-schema-<chart>) on Renovate
// and Dependabot PR branches and pushes the result back onto the branch, so a
// dependency bump in helm/<chart>/values.yaml no longer fails the pre-commit
// check on files only the hooks can rewrite. The hooks themselves and their
// tool pins come from the repo's .pre-commit-config.yaml at run time, so the
// workflow needs no regeneration when a chart is added or a tool is bumped.
func NewHelmDocsRegenInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "helm-docs-regen.yaml"),
		TemplateBody: helmDocsRegenTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":                        params.Header("#", helmDocsRegenTemplateSha),
			templateKeyStepSetUpGitIdentity: params.StepSetUpGitIdentity(),
		},
	}

	return i
}
