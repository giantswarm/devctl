package params

import "github.com/giantswarm/devctl/v7/pkg/gen/internal"

func HasFlavor(p Params, flavor string) bool {
	for _, f := range p.Flavors {
		if f == flavor {
			return true
		}
	}
	return false
}

func Header(comment, githubUrl string) string {
	return internal.Header(comment, githubUrl)
}
