package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
)

//go:embed ensure_major_version_tags.yaml.template
var ensureMajorVersionTagsTemplate string

//go:generate go run ../../../update-template-sha.go ensure_major_version_tags.yaml.template
//go:embed ensure_major_version_tags.yaml.template.sha
var ensureMajorVersionTagsTemplateSha string

func NewEnsureMajorVersionTagsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "ensure_major_version_tags.yaml"),
		TemplateBody: ensureMajorVersionTagsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", ensureMajorVersionTagsTemplateSha),
		},
	}

	return i
}
