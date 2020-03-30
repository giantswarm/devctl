package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

func NewErrorInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "error.go"),
		TemplateBody: errorTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(p),
		},
	}

	return i
}

var errorTemplate = `package {{ .Package }}

import (
	"github.com/giantswarm/microerror"
)

var wrongTypeError = &microerror.Error{
	Kind: "wrongTypeError",
}

func IsWrongTypeError(err error) bool {
	return microerror.Cause(err) == wrongTypeError
}
`
