package params

import "github.com/giantswarm/devctl/v7/pkg/gen"

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string

	// Flavours is the type of project that the rules are for.
	Flavours gen.FlavourSlice

	// Language is the language of the repo that the rules are for.
	Language string
}
