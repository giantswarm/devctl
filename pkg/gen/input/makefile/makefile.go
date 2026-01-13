package makefile

import (
	"github.com/giantswarm/devctl/v7/pkg/gen"
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/makefile/internal/file"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/makefile/internal/params"
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

func (m *Makefile) MakefileGenChainsaw() []input.Input {
	return []input.Input{
		file.NewMakefileGenChainsawMkInput(m.params),
		file.NewChainsawTestStepTemplate(m.params),
		file.NewChainsawTestExampleTest(m.params),
		file.NewChainsawHackKindSetup(m.params),
	}
}

func (m *Makefile) MakefileGenGo() []input.Input {
	return file.NewMakefileGenGoMkInput(m.params)
}

func (m *Makefile) MakefileGenKubernetesAPI() []input.Input {
	return []input.Input{
		file.NewMakefileGenKubernetesAPIMkInput(m.params),
		file.NewHackGitignore(m.params),
		file.NewHackBoilerplate(m.params),
		{
			Delete: true,
			Path:   "Makefile.custom.mk",
		},
		{
			Delete: true,
			Path:   "hack/tools/bin/controller-gen",
		},
		{
			Delete: true,
			Path:   "hack/tools/bin",
		},
		{
			Delete: true,
			Path:   "hack/tools/controller-gen/go.mod",
		},
		{
			Delete: true,
			Path:   "hack/tools/controller-gen/go.sum",
		},
		{
			Delete: true,
			Path:   "hack/tools/controller-gen/tools.go",
		},
		{
			Delete: true,
			Path:   "hack/tools/controller-gen",
		},
		{
			Delete: true,
			Path:   "hack/tools",
		},
	}
}

func (m *Makefile) MakefileGenClusterApp() input.Input {
	return file.NewMakefileGenClusterAppMkInput(m.params)
}
