package dependabot

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/dependabot/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/dependabot/internal/params"
)

type Config struct {
	Daily     bool
	Reviewers []string
}

type Dependabot struct {
	params params.Params
}

func New(config Config) (*Dependabot, error) {
	w := &Dependabot{
		params: params.Params{
			Dir: ".github/",

			Daily:     config.Daily,
			Reviewers: config.Reviewers,
		},
	}

	return w, nil
}

func (d *Dependabot) CreateDependabot() input.Input {
	return file.NewCreateDependabotInput(d.params)
}

func (d *Dependabot) CreateWorkflow() input.Input {
	return file.NewCreateWorkflowInput(d.params)
}
