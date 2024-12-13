package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.app.mk.template
var makefileGenAppMkTemplate string

//go:generate go run ../../../update-template-sha.go Makefile.gen.app.mk.template
//go:embed Makefile.gen.app.mk.template.sha
var makefileGenAppMkTemplateSha string

func NewMakefileGenAppMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.app.mk",
		TemplateBody: makefileGenAppMkTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", makefileGenAppMkTemplateSha),
		},
	}

	return i
}
