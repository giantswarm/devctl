package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/apptest/internal/params"
)

//go:embed basic_suite.go.template
var createBasicSuiteTestTemplate string

func NewCreateBasicSuiteTestInput(p params.Params) input.Input {
	i := input.Input{
		Path:           filepath.Join(p.Dir, "suites/basic", "basic_suite_test.go"),
		TemplateBody:   createBasicSuiteTestTemplate,
		TemplateData:   map[string]interface{}{},
		SkipRegenCheck: true,
	}

	return i
}
