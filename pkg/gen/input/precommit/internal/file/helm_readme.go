package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

//go:embed helm-readme.gotmpl.template
var createHelmReadmeTemplate string

func NewCreateHelmReadmeInput(p params.Params, chartName string) input.Input {
	return input.Input{
		Path:         filepath.Join(p.Dir, "helm", chartName, "README.md.gotmpl"),
		TemplateBody: createHelmReadmeTemplate,
		// Use non-default delimiters so Go's template engine does not interpret
		// the helm-docs {{ template "..." }} directives in the file content.
		TemplateDelims: input.InputTemplateDelims{Left: "[[", Right: "]]"},
	}
}
