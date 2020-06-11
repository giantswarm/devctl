package crud

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/crud/internal/file"
)

type CRUD struct {
}

func NewCRUD() (*CRUD, error) {
	return &CRUD{}, nil
}

func (m *CRUD) CRUD() input.Input {
	return file.NewCRUDInput()
}

func (m *CRUD) Patch() input.Input {
	return file.NewPatchInput()
}
