package workflows

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
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

type Workflows struct {
	params params.Params
}

func New(config Config) (*Workflows, error) {
	w := &Workflows{
		params: params.Params{
			Dir: ".github/workflows",

			CurrentFlavour:  config.Flavour,
			FlavourApp:      FlavourApp,
			FlavourCLI:      FlavourCLI,
			FlavourLibrary:  FlavourLibrary,
			FlavourOperator: FlavourOperator,
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
