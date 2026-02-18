package params

import (
	"github.com/giantswarm/devctl/v7/pkg/gen"
	"github.com/giantswarm/devctl/v7/pkg/gen/internal"
)

func Header(comment, githubUrl string) string {
	return internal.Header(comment, githubUrl)
}

func StepSetUpGitIdentity() string {
	return internal.StepSetUpGitIdentity()
}

func IsFlavourCLI(p Params) bool {
	return p.Flavours.Contains(gen.FlavourCLI)
}

func Package(p Params) string {
	return internal.Package(p.Dir)
}

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName(p.Dir, suffix)
}
