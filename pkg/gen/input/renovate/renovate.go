package renovate

import (
	"github.com/giantswarm/devctl/v2/pkg/gen/input"
	"github.com/giantswarm/devctl/v2/pkg/gen/input/renovate/internal/file"
	"github.com/giantswarm/devctl/v2/pkg/gen/input/renovate/internal/params"
)

type Config struct {
	Interval string
	Language string
	Reviewer string
}

type Renovate struct {
	params params.Params
}

func New(config Config) (*Renovate, error) {
	w := &Renovate{
		params: params.Params{
			Dir: "",

			Interval: config.Interval,
			Language: config.Language,
			Reviewer: config.Reviewer,
		},
	}

	return w, nil
}

func (d *Renovate) CreateRenovate() input.Input {
	return file.NewCreateRenovateInput(d.params)
}
