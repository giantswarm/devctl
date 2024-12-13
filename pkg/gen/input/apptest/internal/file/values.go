package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/apptest/internal/params"
)

//go:embed values.yaml.template
var createValuesTemplate string

func NewCreateValuesInput(p params.Params) input.Input {
	i := input.Input{
		Path:           filepath.Join(p.Dir, "suites/basic", "values.yaml"),
		TemplateBody:   createValuesTemplate,
		TemplateData:   map[string]interface{}{},
		SkipRegenCheck: true,
	}

	return i
}
