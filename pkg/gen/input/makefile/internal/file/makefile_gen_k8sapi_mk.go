package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.k8sapi.mk.template
var makefileGenKubernetesAPITemplate string

func NewMakefileGenKubernetesAPIMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.k8sapi.mk",
		TemplateBody: makefileGenKubernetesAPITemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}
