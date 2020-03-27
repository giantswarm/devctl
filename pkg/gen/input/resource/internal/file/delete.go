package file

import (
	"context"
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

type Delete struct {
	dir string
}

func NewDelete(p params.Params) *Delete {
	f := &Delete{
		dir: p.Dir,
	}

	return f
}

func (f *Delete) GetInput(ctx context.Context) (input.Input, error) {
	i := input.Input{
		Path:         filepath.Join(f.dir, params.RegenerableFileName("delete.go")),
		Scaffolding:  false,
		TemplateBody: deleteTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(f.dir),
		},
	}

	return i, nil
}

var deleteTemplate = `package {{ .Package }}

import (
	"context"

	"github.com/giantswarm/microerror"
)

func (r *Resource) EnsureDeleted(ctx context.Context, obj interface{}) error {
	current, err := r.stateGetter.GetDeleteCurrentState(ctx, obj)
	if err != nil {
		return microerror.Mask(err)
	}
	desired, err := r.stateGetter.GetDeleteDesiredState(ctx, obj)
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
