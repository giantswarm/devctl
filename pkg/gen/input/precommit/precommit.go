package precommit

import (
	"os"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/file"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

type Config struct {
	Language         string
	Flavors          []string
	RepoName         string
	K8sSchemaVersion string
	// NodeLintTarget and NodeFormatTarget are the package.json lint/format-check
	// scripts the dev-only pre-push hooks run (Node only). Empty omits the
	// respective hook. They are repo-owned because every Node repo ships a
	// bespoke eslint/prettier toolchain under a different script name (backstage:
	// lint:all / prettier:check; happa: lint / validate-prettier), so they are
	// configured per repo rather than baked into the generator.
	NodeLintTarget   string
	NodeFormatTarget string
}

type PreCommit struct {
	params params.Params
}

func New(config Config) (*PreCommit, error) {
	workingDir := "."

	p := params.Params{
		Dir:              "",
		Language:         config.Language,
		Flavors:          config.Flavors,
		RepoName:         config.RepoName,
		WorkingDir:       workingDir,
		K8sSchemaVersion: config.K8sSchemaVersion,
	}

	if params.HasFlavor(p, "helmchart") {
		helmCharts, err := file.FindHelmCharts(workingDir)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		p.HelmCharts = helmCharts
	}

	// Dev-only Node lint/format hooks: emitted only when a target script is
	// configured. The run prefix is detected from the lockfile, mirroring the
	// circleci generator's package-manager probe.
	if config.Language == "node" && (config.NodeLintTarget != "" || config.NodeFormatTarget != "") {
		p.NodeRunPrefix = nodeRunPrefix(workingDir)
		p.NodeLintTarget = config.NodeLintTarget
		p.NodeFormatTarget = config.NodeFormatTarget
	}

	return &PreCommit{params: p}, nil
}

// nodeRunPrefix returns the package-manager script-run prefix for the lockfile
// present in dir. Mirrors the circleci generator's detectPackageManager probe;
// kept local to avoid a dependency on the circleci input package. Defaults to
// "yarn run" (Berry is the unset default there too).
func nodeRunPrefix(dir string) string {
	if _, err := os.Stat(dir + "/package-lock.json"); err == nil {
		return "npm run"
	}
	if _, err := os.Stat(dir + "/pnpm-lock.yaml"); err == nil {
		return "pnpm run"
	}
	if _, err := os.Stat(dir + "/yarn.lock"); err == nil {
		return "yarn run"
	}
	// No lockfile (e.g. dry-run/tests): fall back to npm, the most portable.
	return "npm run"
}

func (p *PreCommit) CreatePreCommitConfig() input.Input {
	return file.NewCreatePreCommitConfigInput(p.params)
}

func (p *PreCommit) CreatePreCommitAction() input.Input {
	return file.NewCreatePreCommitActionInput(p.params)
}

func (p *PreCommit) CreateSchemaYamlInputs() []input.Input {
	var inputs []input.Input
	for _, chartName := range p.params.HelmCharts {
		inputs = append(inputs, file.NewCreateSchemaYamlInput(p.params, chartName))
	}
	return inputs
}

func (p *PreCommit) CreateHelmReadmeInputs() []input.Input {
	var inputs []input.Input
	for _, chartName := range p.params.HelmCharts {
		inputs = append(inputs, file.NewCreateHelmReadmeInput(p.params, chartName))
	}
	return inputs
}
