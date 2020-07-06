package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/project/internal/params"
)

func NewProjectInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.FileName(p, "project.go"),
		TemplateBody: projectTemplate,
		TemplateData: map[string]interface{}{
			"Name": params.Name(p),
		},
	}

	return i
}

var projectTemplate = `package project

const (
	// TODO Update project's description.
	description = "{{ .Name }} description."
)
`
