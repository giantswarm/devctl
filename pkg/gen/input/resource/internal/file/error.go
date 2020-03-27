package file

import (
	"context"
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

type Error struct {
	dir string
}

func NewError(p params.Params) *Error {
	f := &Error{
		dir: p.Dir,
	}

	return f
}

func (f *Error) GetInput(ctx context.Context) (input.Input, error) {
	i := input.Input{
		Path:         filepath.Join(f.dir, params.RegenerableFileName("error.go")),
		Scaffolding:  false,
		TemplateBody: errorTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(f.dir),
		},
	}

	return i, nil
}

var errorTemplate = `package {{ .Package }}

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}
`
