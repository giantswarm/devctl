package workflows

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/enum/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/enum/internal/params"
	"github.com/giantswarm/microerror"
)

type Config struct {
	Dir    string
	Type   string
	Values []string
}

type Workflows struct {
	params params.Params
}

func New(config Config) (*Workflows, error) {
	if config.Dir == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Dir must not be empty", config)
	}
	if config.Type == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Type must not be empty", config)
	}
	if len(config.Values) == 0 {
		return nil, microerror.Maskf(invalidConfigError, "%T.Values must not be empty", config)
	}

	w := &Workflows{
		params: params.Params{
			Dir:    config.Dir,
			Type:   config.Type,
			Values: config.Values,
		},
	}

	return w, nil
}

func (w *Workflows) Enum() input.Input {
	return file.NewEnumInput(w.params)
}
