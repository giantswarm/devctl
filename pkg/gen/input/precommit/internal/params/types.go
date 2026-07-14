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
	// "yarn run" / "pnpm run") the dev-only Node lint hook invokes. Set only
	// when NodeDevLintHook is true; detected from the lockfile.
	NodeRunPrefix string
	// NodeDevLintHook turns on the dev-only pre-push `ci:lint` hook (Node only).
	// The hook runs the repo's standard `ci:lint` script -- a single convention
	// name, like ci:verify/ci:build, that the repo defines pointing at its own
	// eslint/prettier toolchain. No per-script knob: the repo converges its
	// scripts to the convention, the generator does not bend to the repo.
	NodeDevLintHook bool
}
