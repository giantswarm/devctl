package params

import "github.com/giantswarm/devctl/pkg/gen"

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string

	Flavour gen.Flavour
}
