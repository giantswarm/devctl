package params

import "github.com/giantswarm/devctl/v2/pkg/gen"

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string

	EnableFloatingMajorVersionTags bool

	Flavours gen.FlavourSlice
}
