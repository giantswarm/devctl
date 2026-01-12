package file

import (
	_ "embed"
	"os"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
	"github.com/giantswarm/devctl/v7/pkg/gen/internal"
)

//go:embed test-kyverno-policies-with-chainsaw.yaml.template
var testKyvernoPoliciesWithChainsawTemplate string

//go:generate go run ../../../update-template-sha.go test-kyverno-policies-with-chainsaw.yaml.template
//go:embed test-kyverno-policies-with-chainsaw.yaml.template.sha
var testKyvernoPoliciesWithChainsawTemplateSha string

func NewTestKyvernoPoliciesWithChainsawInput(p params.Params) input.Input {
	// Get repository name from current working directory
	wd, err := os.Getwd()
	if err != nil {
		// Fallback to using internal.Package with "."
		wd = "."
	}
	repository := internal.Package(wd)

	i := input.Input{
		Path:         params.RegenerableFileName(p, "test-kyverno-policies-with-chainsaw.yaml"),
		TemplateBody: testKyvernoPoliciesWithChainsawTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":     params.Header("#", testKyvernoPoliciesWithChainsawTemplateSha),
			"Repository": repository,
		},
	}

	return i
}
