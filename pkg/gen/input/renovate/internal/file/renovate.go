package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/renovate/internal/params"
)

//go:embed renovate.json5.template
var createRenovateTemplate string

func NewCreateRenovateInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, "renovate.json5"),
		TemplateBody: createRenovateTemplate,
		TemplateData: map[string]interface{}{
			"Interval": params.Interval(p),
			"Language": params.Language(p),
		},
	}

	return i
}
