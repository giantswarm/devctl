package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string
	// Language is the primary language of the target repository (e.g. "go", "generic").
	Language string
	// Flavors is a list of additional checker flavors to include (e.g. "bash", "md", "helmchart").
	Flavors []string
}
