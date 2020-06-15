package workflows

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

type Config struct {
	Dir string
}

type Workflows struct {
	params params.Params
}

func New(config Config) (*Workflows, error) {
	if config.Dir == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Dir must not be empty", config)
	}

	w := &Workflows{
		params: params.Params{
			Dir: config.Dir,
		},
	}

	return w, nil
}

func (w *Workflows) CreateRelease() input.Input {
	return file.NewCreateReleaseInput(w.params)
}

func (w *Workflows) CreateReleasePR() input.Input {
	return file.NewCreateReleasePRInput(w.params)
}
