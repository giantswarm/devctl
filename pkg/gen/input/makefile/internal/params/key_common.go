package params

import (
	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func IsFlavourApp(p Params) bool {
	return p.Flavour == gen.FlavourApp
}

func IsFlavourCLI(p Params) bool {
	return p.Flavour == gen.FlavourCLI
}

func FileName(p Params, suffix string) string {
	return internal.FileName("pkg/project", suffix)
}

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName("pkg/project", suffix)
}
