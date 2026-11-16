package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed validate_organization_kustomizations.yaml.template
var validateOrganizationKustomizationsTemplate string

//go:generate go run ../../../update-template-sha.go validate_organization_kustomizations.yaml.template
//go:embed validate_organization_kustomizations.yaml.template*
var validateOrganizationKustomizationsTemplateFiles embed.FS

var validateOrganizationKustomizationsTemplateSha = input.TemplateSHA(validateOrganizationKustomizationsTemplateFiles, "validate_organization_kustomizations.yaml.template")

// NewValidateOrganizationKustomizationsInput is the check of a management-clusters
// repository's Agent Platform organization paths, run on every pull request.
func NewValidateOrganizationKustomizationsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "validate_organization_kustomizations.yaml"),
		TemplateBody: validateOrganizationKustomizationsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", validateOrganizationKustomizationsTemplateSha),
		},
	}

	return i
}
