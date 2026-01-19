package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/makefile/internal/params"
)

//go:embed Makefile.gen.chainsaw.mk.template
var makefileGenChainsawMkTemplate string

//go:generate go run ../../../update-template-sha.go Makefile.gen.chainsaw.mk.template
//go:embed Makefile.gen.chainsaw.mk.template.sha
var makefileGenChainsawMkTemplateSha string

func NewMakefileGenChainsawMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.chainsaw.mk",
		TemplateBody: makefileGenChainsawMkTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", makefileGenChainsawMkTemplateSha),
		},
	}

	return i
}
