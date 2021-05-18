package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.template
var makefileTemplate string

func NewMakefileInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile",
		TemplateBody: makefileTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}
