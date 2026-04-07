package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

//go:embed schema.yaml.template
var createSchemaYamlTemplate string

const k8sSchemaVersion = "v1.33.1"

func NewCreateSchemaYamlInput(p params.Params, chartName string) input.Input {
	return input.Input{
		Path:         filepath.Join(p.Dir, "helm", chartName, ".schema.yaml"),
		TemplateBody: createSchemaYamlTemplate,
		TemplateData: map[string]interface{}{
			"ChartName":        chartName,
			"K8sSchemaVersion": k8sSchemaVersion,
		},
	}
}
