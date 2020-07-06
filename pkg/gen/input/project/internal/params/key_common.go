package params

import (
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func Name(p Params) string {
	return filepath.Base(p.GoModule)
}

func Module(p Params) string {
	return p.GoModule
}

func FileName(p Params, suffix string) string {
	return internal.FileName("pkg/project", suffix)
}

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName("pkg/project", suffix)
}
