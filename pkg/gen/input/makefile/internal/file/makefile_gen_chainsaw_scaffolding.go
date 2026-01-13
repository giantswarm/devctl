package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/makefile/internal/params"
)

//go:embed chainsaw-hack-kind-setup.sh.template
var chainsawHackScaffolding string

//go:generate go run ../../../update-template-sha.go chainsaw-hack-kind-setup.sh.template
//go:embed chainsaw-hack-kind-setup.sh.template.sha
var chainsawHackScaffoldingSha string

func NewChainsawHackKindSetup(p params.Params) input.Input {
	i := input.Input{
		Path:         "hack/kind-with-registry.sh",
		TemplateBody: chainsawHackScaffolding,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", chainsawHackScaffoldingSha),
		},
	}

	return i
}

//go:embed chainsaw-test-steps-template.yaml.template
var chainsawTestStepTemplate string

func NewChainsawTestStepTemplate(p params.Params) input.Input {
	i := input.Input{
		Path:         "tests/chainsaw/_steps-templates/cluster-policy-ready.yaml",
		TemplateBody: chainsawTestStepTemplate,
	}

	return i
}

//go:embed chainsaw-test-policy-ready.yaml.template
var chainsawTestPolicyReadyTemplate string

func NewChainsawTestExampleTest(p params.Params) input.Input {
	i := input.Input{
		Path:         "tests/chainsaw/check-policy-ready/check-policy-ready.yaml",
		TemplateBody: chainsawTestPolicyReadyTemplate,
	}

	return i
}
