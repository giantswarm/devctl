package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

//go:embed ensure_major_version_tags.yaml.template
var ensureMajorVersionTagsTemplate string

func NewEnsureMajorVersionTagsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "ensure_major_version_tags.yaml"),
		TemplateBody: ensureMajorVersionTagsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}
