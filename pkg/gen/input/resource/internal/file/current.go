package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

func NewCurrentInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "current.go"),
		TemplateBody: currentTemplate,
		TemplateData: map[string]interface{}{
			"StateImport":      params.StateImport(p),
			"StateImportAlias": params.StateImportAlias(p),
			"StateType":        params.StateType(p),
			"Package":          params.Package(p),
		},
	}

	return i
}

var currentTemplate = `package {{ .Package }}

import (
	"context"

	"github.com/giantswarm/microerror"
	{{ if .StateImportAlias }}{{ .StateImportAlias }} {{ end }}"{{ .StateImport }}"
)

func (r *Resource) GetCurrentState(ctx context.Context, obj interface{}) (interface{}, error) {
	typedObj, err := toTypedObject(obj)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	// For type safety.
	var state []*{{ .StateType }}

	state, err = r.getCurrentState(ctx, typedObj)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return state, nil
}
`
