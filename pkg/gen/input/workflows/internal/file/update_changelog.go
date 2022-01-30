package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

//go:embed update_changelog.yaml.template
var updateChangelogTemplate string

func NewUpdateChangelogInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "update_changelog.yaml"),
		TemplateBody: updateChangelogTemplate,
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
