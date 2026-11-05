package params

import "github.com/giantswarm/devctl/v8/pkg/gen"

type Params struct {
	// Flavours is the type of project that the rules are for.
	Flavours gen.FlavourSlice

	// Language is the language of the repo that the rules are for.
	Language string
}
