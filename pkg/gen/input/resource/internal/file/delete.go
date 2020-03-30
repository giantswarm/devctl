package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

func NewDeleteInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "delete.go"),
		TemplateBody: deleteTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(p),
		},
	}

	return i
}

var deleteTemplate = `package {{ .Package }}

import (
	"context"

	"github.com/giantswarm/microerror"
)

func (r *Resource) ApplyDeleteChange(ctx context.Context, obj, deleteChange interface{}) error {
	typedObj, err := toTypedObject(obj)
	if err != nil {
		return microerror.Mask(err)
	}
	toDelete, err := toTypedState(deleteChange)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.applyDeleteChange(ctx, typedObj, toDelete)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
`
