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
	// RepoName is the repository name under the giantswarm organization. It is
	// only used to build the `github>giantswarm/<repo>:renovate-custom.json5`
	// extends entry when HasCustomConfig is true.
	RepoName string
	// HasCustomConfig indicates the repo carries an optional repo-owned
	// renovate-custom.json5 next to the generated renovate.json5. When true,
	// it is appended as the last `extends` entry so repo-specific rules win
	// over the shared presets. devctl never generates or touches that file.
	HasCustomConfig bool
}
