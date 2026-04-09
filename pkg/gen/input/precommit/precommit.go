package precommit

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/file"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

type Config struct {
	Language         string
	Flavors          []string
	RepoName         string
	K8sSchemaVersion string
}

type PreCommit struct {
	params params.Params
}

func New(config Config) (*PreCommit, error) {
	workingDir := "."

	p := params.Params{
		Dir:              "",
		Language:         config.Language,
		Flavors:          config.Flavors,
		RepoName:         config.RepoName,
		WorkingDir:       workingDir,
		K8sSchemaVersion: config.K8sSchemaVersion,
	}

	if params.HasFlavor(p, "helmchart") {
		helmCharts, err := file.FindHelmCharts(workingDir)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		p.HelmCharts = helmCharts
	}

	return &PreCommit{params: p}, nil
}

func (p *PreCommit) CreatePreCommitConfig() input.Input {
	return file.NewCreatePreCommitConfigInput(p.params)
}

func (p *PreCommit) CreatePreCommitAction() input.Input {
	return file.NewCreatePreCommitActionInput(p.params)
}

func (p *PreCommit) CreateSchemaYamlInputs() []input.Input {
	var inputs []input.Input
	for _, chartName := range p.params.HelmCharts {
		inputs = append(inputs, file.NewCreateSchemaYamlInput(p.params, chartName))
	}
	return inputs
}

func (p *PreCommit) CreateHelmReadmeInputs() []input.Input {
	var inputs []input.Input
	for _, chartName := range p.params.HelmCharts {
		inputs = append(inputs, file.NewCreateHelmReadmeInput(p.params, chartName))
	}
	return inputs
}
