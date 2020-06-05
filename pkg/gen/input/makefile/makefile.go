package makefile

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/file"
)

type Makefile struct {
}

func New() (*Makefile, error) {
	return &Makefile{}, nil
}

func (m *Makefile) Makefile() input.Input {
	return file.NewMakefileInput()
}
