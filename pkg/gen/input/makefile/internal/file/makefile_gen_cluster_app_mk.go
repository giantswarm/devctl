package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.cluster_app.mk.template
var makefileGenClusterAppMkTemplate string

func NewMakefileGenClusterAppMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.cluster_app.mk",
		TemplateBody: makefileGenClusterAppMkTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}
