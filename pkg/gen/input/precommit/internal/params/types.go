package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string
	// Language is the primary language of the target repository (e.g. "go", "generic").
	Language string
	// Flavors is a list of additional checker flavors to include (e.g. "bash", "md", "helmchart").
	Flavors []string
	// RepoName is the name of the repository under giantswarm organization (e.g. "devctl").
	RepoName string
	// WorkingDir is the root directory of the repository (used for detecting helm charts).
	WorkingDir string
	// HelmCharts is the list of helm chart names discovered under helm/ in WorkingDir.
	HelmCharts []string
	// K8sSchemaVersion is the Kubernetes JSON schema version used in helm chart .schema.yaml.
	K8sSchemaVersion string
	// NodeRunPrefix is the package-manager script-run prefix ("npm run" /
	// "yarn run" / "pnpm run") the dev-only Node lint/format hooks invoke. Set
	// only for Node repos that configure NodeLintTarget/NodeFormatTarget;
	// detected from the lockfile.
	NodeRunPrefix string
	// NodeLintTarget is the package.json lint script the dev-only pre-push lint
	// hook runs (e.g. "lint", "lint:all"). Empty omits the hook. Node only.
	NodeLintTarget string
	// NodeFormatTarget is the package.json format-check script the dev-only
	// pre-push format hook runs (e.g. "prettier:check", "validate-prettier").
	// Empty omits the hook. Node only.
	NodeFormatTarget string
}
