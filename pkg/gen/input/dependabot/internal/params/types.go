package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string
	// Ecosystems contains the ecosystem for each one package manager that you want GitHub Dependabot to monitor for new versions
	Ecosystems []string
	// Interval to check for daily, weekly, or monthly updates (default: weekly).
	Interval string
	// Reviewers is a set of people or teams who are assigned as reviewers.
	Reviewers []string
}
