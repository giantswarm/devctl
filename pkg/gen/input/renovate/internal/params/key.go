package params

func Interval(p Params) string {
	return p.Interval
}

func Language(p Params) string {
	return p.Language
}

func Reviewers(p Params) []string {
	return p.Reviewers
}

func CircleCIGenerated(p Params) bool {
	return p.CircleCIGenerated
}
