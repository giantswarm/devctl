package file

import (
	"text/template"

	"github.com/huandu/xstrings"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/enum/internal/params"
)

func NewCreateReleaseInput(p params.Params) input.Input {
	i := input.Input{
		Path:             params.RegenerableFileName(p, params.FileNameSuffix(p)),
		PostProcessGoFmt: true,
		TemplateBody:     createReleaseTemplate,
		TemplateData: map[string]interface{}{
			"Header":     params.Header("//"),
			"Package":    params.Package(p),
			"Type":       params.Type(p),
			"TypePlural": params.TypePlural(p),
			"Values":     params.Values(p),
		},
		TemplateFuncs: template.FuncMap{
			"camel": xstrings.ToCamelCase,
		},
	}

	return i
}

var createReleaseTemplate = `{{ .Header }}
{{- $type := .Type }}

package {{ .Package }}

import (
	"strings"

	"github.com/giantswarm/microerror"
)

const (
{{- range $value := .Values }}
	{{ $type }}{{ camel $value }} {{ $type }} = "{{ $value }}"
{{- end }}
)

type {{ .Type }} string

func New{{ .Type }}(s string) ({{ .Type }}, error) {
	switch s {
{{- range $value := .Values }}
	case {{ $type }}{{ camel $value }}.String():
		return {{ $type }}{{ camel $value }}, nil
{{- end }}
	}
	return {{ .Type }}(""), microerror.Maskf(invalidConfigError, "flavour must be one of %s", strings.Join(All{{ .TypePlural }}(), "|"))
}

func (e {{ .Type }}) String() string {
	return string(e)
}

type {{ .TypePlural }} []{{ .Type }}

func All{{ .TypePlural }}() []string {
	return []string{
	{{- range $value := .Values }}
		{{ $type }}{{ camel $value }}App.String(),
	{{ - end }}
	}
}

func (es {{ .TypePlural }}) ToStringSlice() []string {
	var ss []string
	for _, e := range es {
		ss = append(ss, e.String())
	}
	return ss
}

func IsValid{{ .Type }}(s string) bool {
	_, err := New{{ .Type }}(s)
	return err == nil
}
`
