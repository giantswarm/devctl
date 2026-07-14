package params

import "github.com/giantswarm/devctl/v8/pkg/gen"

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string

	Flavours gen.FlavourSlice
}
