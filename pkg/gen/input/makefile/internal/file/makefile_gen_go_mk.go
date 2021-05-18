package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.go.mk.template
var makefileGenGoMkTemplate string

func NewMakefileGenGoMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.go.mk",
		TemplateBody: makefileGenGoMkTemplate,
		TemplateData: map[string]interface{}{
			"IsFlavourCLI": params.IsFlavourCLI(p),
			"Header":       params.Header("#"),
		},
	}

	return i
}
