package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

func NewPatchInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "patch.go"),
		TemplateBody: patchTemplate,
		TemplateData: map[string]interface{}{
			"Package":          params.Package(p),
			"StateImport":      params.StateImport(p),
			"StateImportAlias": params.StateImportAlias(p),
			"StateType":        params.StateType(p),
		},
	}

	return i
}

var patchTemplate = `package {{ .Package }}

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/operatorkit/resource/crud"
	{{ if .StateImportAlias }}{{ .StateImportAlias }} {{ end }}"{{ .StateImport }}"
)

func (r *Resource) NewUpdatePatch(ctx context.Context, obj, currentState, desiredState interface{}) (*crud.Patch, error) {
	typedObj, err := toTypedObject(obj)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	current, err := toTypedState(currentState)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	desired, err := toTypedState(desiredState)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	// To add some type safety.
	var create, delete, update []{{ .StateType }}

	create, err = r.newCreateChange(ctx, typedObj, current, desired)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	delete, err = r.newDeleteChangeForUpdatePatch(ctx, typedObj, current, desired)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	update, err = r.newUpdateChange(ctx, typedObj, current, desired)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	patch := crud.NewPatch()
	patch.SetCreateChange(create)
	patch.SetDeleteChange(delete)
	patch.SetUpdateChange(update)

	return patch, nil
}

func (r *Resource) NewDeletePatch(ctx context.Context, obj, currentState, desiredState interface{}) (*crud.Patch, error) {
	typedObj, err := toTypedObject(obj)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	current, err := toTypedState(currentState)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	desired, err := toTypedState(desiredState)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	// To add some type safety.
	var delete []{{ .StateType }}

	delete, err = r.newDeleteChangeForDeletePatch(ctx, typedObj, current, desired)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	patch := crud.NewPatch()
	patch.SetDeleteChange(delete)

	return patch, nil
}
`
