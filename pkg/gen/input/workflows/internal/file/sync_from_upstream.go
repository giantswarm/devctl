package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed sync_from_upstream.yaml.template
var syncFromUpstreamTemplate string

//go:generate go run ../../../update-template-sha.go sync_from_upstream.yaml.template
//go:embed sync_from_upstream.yaml.template.sha
var syncFromUpstreamTemplateSha string

func NewSyncFromUpstreamInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "sync_from_upstream.yaml"),
		TemplateBody: syncFromUpstreamTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", syncFromUpstreamTemplateSha),
		},
	}

	return i
}
