package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed check_readme_links.yaml.template
var checkReadmeLinksTemplate string

//go:generate go run ../../../update-template-sha.go check_readme_links.yaml.template
//go:embed check_readme_links.yaml.template*
var checkReadmeLinksTemplateFiles embed.FS

var checkReadmeLinksTemplateSha = input.TemplateSHA(checkReadmeLinksTemplateFiles, "check_readme_links.yaml.template")

func NewCheckReadmeLinksInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "check_readme_links.yaml"),
		TemplateBody: checkReadmeLinksTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", checkReadmeLinksTemplateSha),
		},
	}

	return i
}
