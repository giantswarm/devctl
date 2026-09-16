package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.k8sapi.mk.template
var makefileGenKubernetesAPITemplate string

//go:generate go run ../../../update-template-sha.go Makefile.gen.k8sapi.mk.template
//go:embed Makefile.gen.k8sapi.mk.template*
var makefileGenKubernetesAPITemplateFiles embed.FS

var makefileGenKubernetesAPITemplateSha = input.TemplateSHA(makefileGenKubernetesAPITemplateFiles, "Makefile.gen.k8sapi.mk.template")

func NewMakefileGenKubernetesAPIMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.k8sapi.mk",
		TemplateBody: makefileGenKubernetesAPITemplate,
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", makefileGenKubernetesAPITemplateSha),
		},
	}

	return i
}
