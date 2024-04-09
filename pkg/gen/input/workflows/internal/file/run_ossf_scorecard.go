package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
)

//go:embed run_ossf_scorecard.yaml.template
var runOSSFScorecardTemplate string

//go:generate go run ../../../update-template-sha.go run_ossf_scorecard.yaml.template
//go:embed run_ossf_scorecard.yaml.template.sha
var runOSSFScorecardTemplateSha string

func NewRunOSSFScorecardInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "run_ossf_scorecard.yaml"),
		TemplateBody: runOSSFScorecardTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", runOSSFScorecardTemplateSha),
		},
	}

	return i
}
