package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/makefile/internal/params"
)

//go:embed chainsaw-tests-steps-template.yaml.template
var chainsawTestsStepTemplate string

func NewChainsawTestsStepTemplate(p params.Params) input.Input {
	i := input.Input{
		Path:         "tests/chainsaw/_steps-templates/cluster-policy-ready.yaml",
		TemplateBody: chainsawTestsStepTemplate,
	}

	return i
}

//go:embed chainsaw-tests-policy-ready.yaml.template
var chainsawTestsPolicyReadyTemplate string

func NewChainsawTestsExampleTest(p params.Params) input.Input {
	i := input.Input{
		Path:         "tests/chainsaw/check-policy-ready/check-policy-ready.yaml",
		TemplateBody: chainsawTestsPolicyReadyTemplate,
	}

	return i
}

//go:embed chainsaw-tests-extra-values.yaml.template
var chainsawTestsExtraValuesTemplate string

func NewChainsawTestsExtraValues(p params.Params) input.Input {
	i := input.Input{
		Path:         "tests/chainsaw/values.yaml",
		TemplateBody: chainsawTestsExtraValuesTemplate,
	}

	return i
}
