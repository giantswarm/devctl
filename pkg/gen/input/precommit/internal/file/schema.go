package file

import (
	"embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

//go:generate go run ../../../update-template-sha.go schema.yaml.template
//go:embed schema.yaml.template
var createSchemaYamlTemplate string

//go:embed schema.yaml.template*
var createSchemaYamlTemplateFiles embed.FS

var createSchemaYamlTemplateSha = input.TemplateSHA(createSchemaYamlTemplateFiles, "schema.yaml.template")

func NewCreateSchemaYamlInput(p params.Params, chartName string) input.Input {
	return input.Input{
		Path:         filepath.Join(p.Dir, "helm", chartName, ".schema.yaml"),
		TemplateBody: createSchemaYamlTemplate,
		TemplateData: map[string]interface{}{
			templateKeyHeader:  params.Header("#", createSchemaYamlTemplateSha),
			"ChartName":        chartName,
			"K8sSchemaVersion": p.K8sSchemaVersion,
		},
	}
}
