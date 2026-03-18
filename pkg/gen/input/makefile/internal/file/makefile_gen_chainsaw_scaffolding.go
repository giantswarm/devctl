package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/makefile/internal/params"
)

//go:embed chainsaw-tests-steps-template.yaml.template
var chainsawTestsStepTemplate string

//go:generate go run ../../../update-template-sha.go chainsaw-tests-steps-template.yaml.template
//go:embed chainsaw-tests-steps-template.yaml.template.sha
var chainsawTestsStepTemplateSha string

func NewChainsawTestsStepTemplate(p params.Params) input.Input {
	i := input.Input{
		Path:         "tests/chainsaw/_steps-templates/cluster-policy-ready.yaml",
		TemplateBody: chainsawTestsStepTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", chainsawTestsStepTemplateSha),
		},
		SkipRegenCheck: true,
	}

	return i
}

//go:embed chainsaw-tests-policy-ready.yaml.template
var chainsawTestsPolicyReadyTemplate string

func NewChainsawTestsExampleTest(p params.Params) input.Input {
	i := input.Input{
		Path:         "tests/chainsaw/check-policy-ready/chainsaw-test.yaml",
		TemplateBody: chainsawTestsPolicyReadyTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", makefileGenChainsawMkTemplateSha),
		},
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

//go:embed hack-chainsaw-extra-resources.sh.template
var chainsawExtraResourcesShTemplate string

func NewChainsawExtraResourcesScript(p params.Params) input.Input {
	return input.Input{
		Path:         "hack/chainsaw-extra-resources.sh",
		TemplateBody: chainsawExtraResourcesShTemplate,
		Permissions:  0755,
	}
}
