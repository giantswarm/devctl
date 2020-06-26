package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/params"
)

func NewMetaInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.FileName(p, "meta.go"),
		TemplateBody: metaTemplate,
		TemplateData: map[string]interface{}{
			"Grave":   "`",
			"Package": params.Package(p),
		},
	}

	return i
}

var metaTemplate = `package {{ .Package }}

const description = {{ .Grave }}Displays this help message.{{ .Grave }}

var examples = []string{
	{{ .Grave }}# Example with comment.{{ .Grave }},
	{{ .Grave }}--example "value"{{ .Grave }},

	{{ .Grave }}--example-persistent "value"{{ .Grave }},

	{{ .Grave }}-e "value" -p "value"{{ .Grave }},

	{{ .Grave }}# Example with multi{{ .Grave }},
	{{ .Grave }}# line comment.{{ .Grave }},
	{{ .Grave }}--example "value"{{ .Grave }},
}
`
