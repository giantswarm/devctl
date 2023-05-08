package params

import (
	"github.com/giantswarm/devctl/v2/pkg/gen"
	"github.com/giantswarm/devctl/v2/pkg/gen/internal"
)

func Header(comment string) string {
	return internal.Header(comment)
}

func StepSetUpGitIdentity() string {
	return internal.StepSetUpGitIdentity()
}

func EnableFloatingMajorVersionTags(p Params) bool {
	return p.EnableFloatingMajorVersionTags
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
