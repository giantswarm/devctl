package params

import "github.com/giantswarm/devctl/v8/pkg/gen"

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string

	Flavours gen.FlavourSlice

	// RepoName is the repository name under the giantswarm organization. It is
	// templated into cliff.toml's [remote.github] section so git-cliff resolves
	// PR links and authors against the consuming repo.
	RepoName string
}
