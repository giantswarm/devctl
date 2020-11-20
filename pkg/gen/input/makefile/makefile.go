package makefile

import (
	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

type Config struct {
	Flavour gen.Flavour
}

type Makefile struct {
	params params.Params
}

func New(config Config) (*Makefile, error) {
	m := &Makefile{
		params: params.Params{
			Flavour: config.Flavour,
		},
	}

	return m, nil
}

func (m *Makefile) Makefile() input.Input {
	return file.NewMakefileInput(m.params)
}

func (m *Makefile) MakefileApp() input.Input {
	return file.NewMakefileAppMkInput(m.params)
}

func (m *Makefile) MakefileGo() input.Input {
	return file.NewMakefileGoMkInput(m.params)
}
