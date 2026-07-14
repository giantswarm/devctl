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

func RepoName(p Params) string {
	return p.RepoName
}

func HasCustomConfig(p Params) bool {
	return p.HasCustomConfig
}

func Deprecated(p Params) bool {
	return p.Deprecated
}
