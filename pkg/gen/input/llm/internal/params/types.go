package params

import "github.com/giantswarm/devctl/v8/pkg/gen"

type Params struct {
	// Flavours are the types of the repo that the instructions are for.
	Flavours gen.FlavourSlice
	// Language is the language of the repo that the instructions are for.
	Language string
}
