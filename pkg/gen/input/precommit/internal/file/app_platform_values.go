package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

//go:generate go run ../../../update-template-sha.go app-platform-values.yaml.template
//go:embed app-platform-values.yaml.template
var createAppPlatformValuesTemplate string

//go:embed app-platform-values.yaml.template.sha
var createAppPlatformValuesTemplateSha string

// NewCreateAppPlatformValuesInput generates the schema-only values file with
// the keys the Giant Swarm app platform injects into every App via the
// <cluster>-cluster-values ConfigMap. It is listed before values.yaml in the
// generated .schema.yaml so the chart's own definitions take precedence.
func NewCreateAppPlatformValuesInput(p params.Params, chartName string) input.Input {
	return input.Input{
		Path:         filepath.Join(p.Dir, "helm", chartName, "zz_generated.app-platform.values.yaml"),
		TemplateBody: createAppPlatformValuesTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", createAppPlatformValuesTemplateSha),
		},
	}
}
