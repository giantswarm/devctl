package makefile

import (
	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

type Config struct {
	Flavours gen.FlavourSlice
}

type Makefile struct {
	params params.Params
}

func New(config Config) (*Makefile, error) {
	m := &Makefile{
		params: params.Params{
			Flavours: config.Flavours,
		},
	}

	return m, nil
}

func (m *Makefile) Makefile() input.Input {
	return file.NewMakefileInput(m.params)
}

func (m *Makefile) MakefileGenApp() input.Input {
	return file.NewMakefileGenAppMkInput(m.params)
}

func (m *Makefile) MakefileGenGo() input.Input {
	return file.NewMakefileGenGoMkInput(m.params)
}
