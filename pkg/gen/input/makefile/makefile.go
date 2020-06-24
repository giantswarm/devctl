package makefile

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

const (
	FlavourApp = iota
	FlavourCLI
	FlavourLibrary
	FlavourOperator
)

type Config struct {
	Flavour int
}

type Makefile struct {
	params params.Params
}

func New(config Config) (*Makefile, error) {
	m := &Makefile{
		params: params.Params{
			CurrentFlavour:  config.Flavour,
			FlavourApp:      FlavourApp,
			FlavourCLI:      FlavourCLI,
			FlavourLibrary:  FlavourLibrary,
			FlavourOperator: FlavourOperator,
		},
	}

	return m, nil
}

func (m *Makefile) Makefile() input.Input {
	return file.NewMakefileInput(m.params)
}
