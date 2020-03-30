package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

func NewCreateInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "create.go"),
		TemplateBody: createTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(p),
		},
	}

	return i
}

var createTemplate = `
package {{ .Package }}

import (
	"context"

	"github.com/giantswarm/microerror"
)

func (r *Resource) ApplyCreateChange(ctx context.Context, obj, createChange interface{}) error {
	typedObj, err := toTypedObject(obj)
	if err != nil {
		return microerror.Mask(err)
	}
	toCreate, err := toTypedState(createChange)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.applyCreateChange(ctx, typedObj, toCreate)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
`
