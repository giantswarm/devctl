package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v2/pkg/gen/input"
	"github.com/giantswarm/devctl/v2/pkg/gen/input/makefile/internal/params"
)

//go:embed hack-.gitignore
var hackGitignore string

func NewHackGitignore(p params.Params) input.Input {
	i := input.Input{
		Path:         "hack/.gitignore",
		TemplateBody: hackGitignore,
	}

	return i
}
