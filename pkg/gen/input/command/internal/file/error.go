package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/params"
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

var errorTemplate = `// DO NOT EDIT. Generated with:
//
//	devctl gen command
//
// If you need to add your own error please create error.go file next to this
// file.
package {{ .Package }}

import (
	"github.com/giantswarm/microerror"
)

// executionFailedError is an error type for situations where Resource execution
// cannot continue and must always fall back to operatorkit.
//
// This error should never be matched against and therefore there is no matcher
// implement. For further information see:
//
//     https://github.com/giantswarm/fmt/blob/master/go/errors.md#matching-errors
//
var executionFailedError = &microerror.Error{
	Kind: "executionFailedError",
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var invalidFlagError = &microerror.Error{
	Kind: "invalidFlagError",
}

// IsInvalidFlag asserts invalidFlagError.
func IsInvalidFlag(err error) bool {
	return microerror.Cause(err) == invalidFlagError
}
`
