package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

func NewMakefileInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile",
		TemplateBody: makefileTemplate,
		TemplateData: map[string]interface{}{},
	}

	return i
}

var makefileTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen makefile
#

include *.mk

.PHONY: help
## help: prints this help message
help:
	@echo "Usage: \n"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'
`
