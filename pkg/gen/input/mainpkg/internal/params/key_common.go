package params

import (
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func Name(p Params) string {
	return filepath.Base(p.GoModule)
}

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName("", suffix)
}
