package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

//go:embed hack-boilerplate.go.txt
var hackBoilerplate string

func NewHackBoilerplate(p params.Params) input.Input {
	i := input.Input{
		Path:         "hack/boilerplate.go.txt",
		TemplateBody: hackBoilerplate,
	}

	return i
}
