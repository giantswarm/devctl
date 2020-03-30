package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

func NewKeyInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "key.go"),
		TemplateBody: keyTemplate,
		TemplateData: map[string]interface{}{
			"Package":           params.Package(p),
			"ObjectImport":      params.ObjectImport(p),
			"ObjectImportAlias": params.ObjectImportAlias(p),
			"ObjectType":        params.ObjectType(p),
			"StateImport":       params.StateImport(p),
			"StateImportAlias":  params.StateImportAlias(p),
			"StateType":         params.StateType(p),
		},
	}

	return i
}

var keyTemplate = `package {{ .Package }}

import (
	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/microerror"
	{{ if .ObjectImportAlias }}{{ .ObjectImportAlias }} {{ end }}"{{ .ObjectImport }}"
	{{ if .StateImportAlias }}{{ .StateImportAlias }} {{ end }}"{{ .StateImport }}"
)

func toTypedObject(v interface{}) ({{ .ObjectType }}, error) {
	typedP, ok := v.(*{{ .ObjectType }})
	if !ok {
		return {{ .ObjectType }}{}, microerror.Maskf(wrongTypeError, "expected '%T', got '%T'", typedP, v)
	}
	typed := *typedP

	return typed, nil
}

func toTypedState(v interface{}) ([]{{ .StateType }}, error) {
	if v == nil {
		return nil, nil
	}

	typed, ok := v.([]{{ .StateType }})
	if !ok {
		return nil, microerror.Maskf(wrongTypeError, "expected '%T', got '%T'", typed, v)
	}

	return typed, nil
}
`
