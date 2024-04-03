package params

import (
	"github.com/giantswarm/devctl/v6/pkg/gen"
	"github.com/giantswarm/devctl/v6/pkg/gen/internal"
)

func IsFlavourCLI(p Params) bool {
	return p.Flavours.Contains(gen.FlavourCLI)
}

func Header(comment, githubUrl string) string {
	return internal.Header(comment, githubUrl)
}

func FileName(p Params, suffix string) string {
	return internal.FileName("pkg/project", suffix)
}

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName("pkg/project", suffix)
}
