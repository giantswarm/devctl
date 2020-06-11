package crud

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/crud/internal/file"
)

type Patch struct {
}

func NewPatch() (*Patch, error) {
	return &Patch{}, nil
}

func (m *Patch) Patch() input.Input {
	return file.NewPatchInput()
}
