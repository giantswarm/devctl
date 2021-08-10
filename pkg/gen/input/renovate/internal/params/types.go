package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string
	// Interval to check for daily, weekly, or monthly updates (default: weekly).
	Interval string
	Language string
	// Reviewer is a set of people or teams who are assigned as reviewers.
	Reviewer string
}
