package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/renovate/internal/params"
)

//go:embed renovate.json.template
var createRenovateTemplate string

func NewCreateRenovateInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, "renovate.json"),
		TemplateBody: createRenovateTemplate,
		TemplateData: map[string]interface{}{
			"Interval": params.Interval(p),
			"Language": params.Language(p),
			"Reviewer": params.Reviewer(p),
		},
	}

	return i
}
