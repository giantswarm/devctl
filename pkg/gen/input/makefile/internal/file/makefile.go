package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.template
var makefileTemplate string

//go:generate go run ../../../update-template-sha.go Makefile.template
//go:embed Makefile.template.sha
var makefileTemplateSha string

func NewMakefileInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile",
		TemplateBody: makefileTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", makefileTemplateSha),
		},
	}

	return i
}
