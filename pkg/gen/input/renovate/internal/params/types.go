package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string
	// Interval to check for daily, weekly, or monthly updates (default: weekly).
	Interval string
	Language string
	// Reviewers is the list of reviewers to bake into the generated config's
	// top-level `reviewers` array (e.g. "team:team-rocket"). Empty omits the
	// key entirely.
	Reviewers []string
	// CircleCIGenerated indicates the repo's .circleci/config.yml is generated
	// by `devctl gen circleci`, which bakes in the giantswarm/architect orb
	// version. When true, the generated Renovate config disables Renovate's
	// architect orb updates so they stop fighting align-files regeneration.
	CircleCIGenerated bool
}
