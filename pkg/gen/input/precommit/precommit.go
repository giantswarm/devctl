package precommit

import (
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/file"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

type Config struct {
	Language string
	Flavors  []string
	RepoName string
}

type PreCommit struct {
	params params.Params
}

func New(config Config) (*PreCommit, error) {
	// Get current working directory for detecting repo name and helm charts
	workingDir := "."

	p := &PreCommit{
		params: params.Params{
			Dir:        "",
			Language:   config.Language,
			Flavors:    config.Flavors,
			RepoName:   config.RepoName,
			WorkingDir: workingDir,
		},
	}

	return p, nil
}

func (p *PreCommit) CreatePreCommitConfig() input.Input {
	return file.NewCreatePreCommitConfigInput(p.params)
}
