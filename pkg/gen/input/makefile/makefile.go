package makefile

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/file"
)

const (
	FlavourApp = iota
	FlavourCLI
	FlavourOperator
)

type Makefile struct {
	flavour int
}

func New(flavour int) (*Makefile, error) {
	m := &Makefile{
		flavour: flavour,
	}

	return m, nil
}

func (m *Makefile) Makefile() input.Input {
	c := file.Config{
		CurrentFlavour:  m.flavour,
		FlavourApp:      FlavourApp,
		FlavourCLI:      FlavourCLI,
		FlavourOperator: FlavourOperator,
	}

	return file.NewMakefileInput(c)
}
