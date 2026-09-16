package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.cluster_app.mk.template
var makefileGenClusterAppMkTemplate string

//go:generate go run ../../../update-template-sha.go Makefile.gen.cluster_app.mk.template
//go:embed Makefile.gen.cluster_app.mk.template*
var makefileGenClusterAppMkTemplateFiles embed.FS

var makefileGenClusterAppMkTemplateSha = input.TemplateSHA(makefileGenClusterAppMkTemplateFiles, "Makefile.gen.cluster_app.mk.template")

func NewMakefileGenClusterAppMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.cluster_app.mk",
		TemplateBody: makefileGenClusterAppMkTemplate,
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", makefileGenClusterAppMkTemplateSha),
		},
	}

	return i
}
