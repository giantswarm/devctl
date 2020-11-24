package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/enum/internal/params"
)

func NewCreateReleaseInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, params.FileNameSuffix(p)),
		TemplateBody: createReleaseTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(p),
		},
	}

	return i
}

var createReleaseTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen enum
#
package {{ .Package }}
`
