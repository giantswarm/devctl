package params

type Params struct {
	// Daily interval to check for updates.
	Daily bool
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string
	// Reviewers is a set of people or teams who are assigned as reviewers.
	Reviewers []string
}
