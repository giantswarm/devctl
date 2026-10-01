package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.app.mk.template
var makefileGenAppMkTemplate string

//go:generate go run ../../../update-template-sha.go Makefile.gen.app.mk.template
//go:embed Makefile.gen.app.mk.template*
var makefileGenAppMkTemplateFiles embed.FS

var makefileGenAppMkTemplateSha = input.TemplateSHA(makefileGenAppMkTemplateFiles, "Makefile.gen.app.mk.template")

func NewMakefileGenAppMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.app.mk",
		TemplateBody: makefileGenAppMkTemplate,
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", makefileGenAppMkTemplateSha),
		},
	}

	return i
}
