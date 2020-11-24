package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string

	// Type is the Go type name for this enum.
	Type string

	// Values are allowed enum values.
	Values []string
}
