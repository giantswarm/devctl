package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/project/internal/params"
)

func NewZZProjectInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "project.go"),
		TemplateBody: projectTemplate,
		TemplateData: map[string]interface{}{
			"Name": params.Name(p),
		},
	}

	return i
}

// TODO description should be provided by the user. Or maybe taken from REAMDE.md?
var projectTemplate = `package project

var (
	description = "Command line tool."
	gitSHA      = "n/a"
	name        = "{{ .Name }}"
	source      = "https://github.com/giantswarm/{{ .Name }}"
	version     = "n/a"
)

func Description() string {
	return description
}

func GitSHA() string {
	return gitSHA
}

func Name() string {
	return name
}

func Source() string {
	return source
}

func Version() string {
	return version
}
`
