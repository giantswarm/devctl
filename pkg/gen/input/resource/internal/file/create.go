package file

import (
	"context"
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

type Create struct {
	dir string
}

func NewCreate(p params.Params) *Create {
	f := &Create{
		dir: p.Dir,
	}

	return f
}

func (f *Create) GetInput(ctx context.Context) (input.Input, error) {
	i := input.Input{
		Path:         filepath.Join(f.dir, params.RegenerableFileName("create.go")),
		Scaffolding:  false,
		TemplateBody: createTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(f.dir),
		},
	}

	return i, nil
}

var createTemplate = `
package {{ .Package }}

import (
	"context"

	"github.com/giantswarm/microerror"
)

func (r *Resource) EnsureCreated(ctx context.Context, obj interface{}) error {
	current, err := r.stateGetter.GetCreateCurrentState(ctx, obj)
	if err != nil {
		return microerror.Mask(err)
	}
	desired, err := r.stateGetter.GetCreateDesiredState(ctx, obj)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.ensure(ctx, current, desired)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
`
