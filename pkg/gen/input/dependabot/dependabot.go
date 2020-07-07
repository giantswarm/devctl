package dependabot

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/dependabot/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/dependabot/internal/params"
)

type Config struct {
	Reviewers []string
}

type Dependabot struct {
	params params.Params
}

func New(config Config) (*Dependabot, error) {
	w := &Dependabot{
		params: params.Params{
			Dir: ".github/",

			Reviewers: config.Reviewers,
		},
	}

	return w, nil
}

func (d *Dependabot) CreateDependabot() input.Input {
	return file.NewCreateDependabotInput(d.params)
}
