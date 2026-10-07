package params

import "github.com/giantswarm/devctl/v8/pkg/gen"

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string

	Flavours gen.FlavourSlice

	// ReleaseBranch is the branch whose pushes cut releases in the
	// auto-release workflow.
	ReleaseBranch string

	// RepoName is the repository's name under the giantswarm organization,
	// for cliff.toml's `[remote.github].repo` field.
	RepoName string
}
