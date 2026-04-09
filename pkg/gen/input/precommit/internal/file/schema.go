package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

//go:generate go run ../../../update-template-sha.go schema.yaml.template
//go:embed schema.yaml.template
var createSchemaYamlTemplate string

//go:embed schema.yaml.template.sha
var createSchemaYamlTemplateSha string

func NewCreateSchemaYamlInput(p params.Params, chartName string) input.Input {
	return input.Input{
		Path:         filepath.Join(p.Dir, "helm", chartName, ".schema.yaml"),
		TemplateBody: createSchemaYamlTemplate,
		TemplateData: map[string]interface{}{
			"Header":           params.Header("#", createSchemaYamlTemplateSha),
			"ChartName":        chartName,
			"K8sSchemaVersion": p.K8sSchemaVersion,
		},
	}
}
