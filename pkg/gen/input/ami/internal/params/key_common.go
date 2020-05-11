package params

import (
	"fmt"
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func Package(p Params) string {
	abs, err := filepath.Abs(p.Dir)
	if err != nil {
		panic(fmt.Sprintf("filepath.Abs: %s", err))
	}

	return filepath.Base(abs)
}

func RegenerableFileName(params Params, suffix string) string {
	return filepath.Join(params.Dir, internal.RegenerableFilePrefix+suffix)
}
