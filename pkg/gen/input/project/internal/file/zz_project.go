package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/project/internal/params"
)

func NewZZProjectInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "project.go"),
		TemplateBody: projectZZTemplate,
		TemplateData: map[string]interface{}{
			"Name":   params.Name(p),
			"Module": params.Module(p),
		},
	}

	return i
}

var projectZZTemplate = `package project

const (
	name        = "{{ .Name }}"
	source      = "https://{{ .Module }}"
)

var (
	gitSHA      = "n/a"
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
