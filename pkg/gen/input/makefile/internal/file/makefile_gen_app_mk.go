package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.app.mk.template
var makefileGenAppMkTemplate string

func NewMakefileGenAppMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.app.mk",
		TemplateBody: makefileGenAppMkTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}
