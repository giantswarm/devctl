package apptest

import (
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/apptest/internal/file"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/apptest/internal/params"
)

type Config struct {
	AppName  string
	RepoName string
	Catalog  string
}

type Apptest struct {
	params params.Params
}

func New(config Config) (*Apptest, error) {
	a := &Apptest{
		params: params.Params{
			Dir: "tests/e2e/",

			AppName:  config.AppName,
			RepoName: config.RepoName,
			Catalog:  config.Catalog,
		},
	}

	return a, nil
}

func (a *Apptest) CreateApptest() []input.Input {
	return []input.Input{
		file.NewCreateConfigInput(a.params),
		file.NewCreateBasicSuiteTestInput(a.params),
		file.NewCreateValuesInput(a.params),
		file.NewCreateGoModInput(a.params),
	}
}
