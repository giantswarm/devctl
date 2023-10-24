package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/dependabot/internal/params"
)

//go:embed dependabot.yml.template
var createDependabotTemplate string

func NewCreateDependabotInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, "dependabot.yml"),
		TemplateBody: createDependabotTemplate,
		TemplateData: map[string]interface{}{
			"EcosystemGithubActions": params.EcosystemGithubActions(p),
			"EcosystemGomod":         params.EcosystemGomod(p),
			"Ecosystems":             params.Ecosystems(p),
			"Header":                 params.Header("#"),
			"Interval":               params.Interval(p),
			"Reviewers":              params.Reviewers(p),
		},
	}

	return i
}
