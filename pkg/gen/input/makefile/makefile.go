package makefile

import (
	"context"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

type Makefile struct {
	config Config

	params params.Params
}

func New(config Config) (*Makefile, error) {
	err := config.Validate()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	f := &Makefile{
		config: config,
	}

	return f, nil
}

func (m *Makefile) Makefile() input.Input {
	return file.NewMakefileInput(m.params)
}

func (m *Makefile) Params(ctx context.Context) error {
	err := m.initParams(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	return nil
}

func (m *Makefile) initParams(ctx context.Context) error {
	m.params = params.Params{
		Application: m.config.Application,
		Dir:         "./",
	}

	return nil
}
