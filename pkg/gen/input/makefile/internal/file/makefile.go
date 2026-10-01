package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.template
var makefileTemplate string

//go:generate go run ../../../update-template-sha.go Makefile.template
//go:embed Makefile.template*
var makefileTemplateFiles embed.FS

var makefileTemplateSha = input.TemplateSHA(makefileTemplateFiles, "Makefile.template")

func NewMakefileInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile",
		TemplateBody: makefileTemplate,
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", makefileTemplateSha),
		},
	}

	return i
}
