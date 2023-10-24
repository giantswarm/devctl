package dependabot

import (
	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/dependabot/internal/file"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/dependabot/internal/params"
)

type Config struct {
	Interval   string
	Reviewers  []string
	Ecosystems []string
}

type Dependabot struct {
	params params.Params
}

func New(config Config) (*Dependabot, error) {
	w := &Dependabot{
		params: params.Params{
			Dir: ".github/",

			Ecosystems: config.Ecosystems,
			Interval:   config.Interval,
			Reviewers:  config.Reviewers,
		},
	}

	return w, nil
}

func (d *Dependabot) CreateDependabot() input.Input {
	return file.NewCreateDependabotInput(d.params)
}
