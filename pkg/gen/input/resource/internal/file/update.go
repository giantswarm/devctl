package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

func NewUpdateInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "update.go"),
		TemplateBody: updateTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(p),
		},
	}

	return i
}

var updateTemplate = `package {{ .Package }}

import (
	"context"

	"github.com/giantswarm/microerror"
)

func (r *Resource) ApplyUpdateChange(ctx context.Context, obj, updateChange interface{}) error {
	typedObj, err := toTypedObject(obj)
	if err != nil {
		return microerror.Mask(err)
	}
	toUpdate, err := toTypedState(updateChange)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.applyUpdateChange(ctx, typedObj, toUpdate)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
`
