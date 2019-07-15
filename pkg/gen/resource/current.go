package resource

import (
	"context"
	"path"
	"path/filepath"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
)

type Current struct {
	dir string
}

func NewCurrent(config Config) (*Current, error) {
	err := config.Validate()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	f := &Current{
		dir: config.Dir,
	}

	return f, nil
}

func (f *Current) GetInput(ctx context.Context) (input.Input, error) {
	i := input.Input{
		Path:         filepath.Join(f.dir, "current.go"),
		TemplateBody: currentTemplate,
		TemplateData: map[string]interface{}{
			"Package": path.Base(f.dir),
		},
	}

	return i, nil
}

var currentTemplate = `package {{ .Package }}

import (
	"context"

	"github.com/giantswarm/microerror"
)

func (r *Resource) GetCurrentState(ctx context.Context, obj interface{}) (interface{}, error) {
	state, err := r.stateGetter.GetCurrentState(ctx, obj)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return state, nil
}
`
