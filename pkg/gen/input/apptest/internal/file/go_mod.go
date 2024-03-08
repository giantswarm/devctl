package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/apptest/internal/params"
)

//go:embed go.mod.template
var createGoModTemplate string

func NewCreateGoModInput(p params.Params) input.Input {
	i := input.Input{
		Path:           filepath.Join(p.Dir, "go.mod"),
		TemplateBody:   createGoModTemplate,
		TemplateData:   map[string]interface{}{},
		SkipRegenCheck: true,
	}

	return i
}
