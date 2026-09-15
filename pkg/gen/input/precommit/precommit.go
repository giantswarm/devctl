package precommit

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/giantswarm/microerror"
	"golang.org/x/mod/modfile"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/file"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

// helmValuesSchemaJSONModule and schemalintModule are the module paths the generated
// helm-schema hook pins via additional_dependencies. Their versions are read from go.mod
// (via the running binary's own build info, see pinnedVersions) rather than hardcoded in
// the template, so go.mod stays the single source of truth. See giantswarm/devctl#2195.
const (
	helmValuesSchemaJSONModule = "github.com/losisin/helm-values-schema-json/v2"
	schemalintModule           = "github.com/giantswarm/schemalint/v2"
)

type Config struct {
	Language         string
	Flavors          []string
	RepoName         string
	K8sSchemaVersion string
	GoGenerate       bool
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
		GoGenerate:       config.GoGenerate,
	}

	if params.HasFlavor(p, "helmchart") {
		helmCharts, err := file.FindHelmCharts(workingDir)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		p.HelmCharts = helmCharts

		p.HelmValuesSchemaJSONVersion, p.SchemalintVersion = pinnedVersions()
		if p.HelmValuesSchemaJSONVersion == "" || p.SchemalintVersion == "" {
			return nil, microerror.Maskf(
				executionFailedError,
				"could not determine pinned version of %s or %s from devctl's own build info; is go.mod missing one of them?",
				helmValuesSchemaJSONModule, schemalintModule,
			)
		}
	}

	// Dev-only Node lint hook: a single `ci:lint` pre-push hook for every Node
	// repo (the convention, no per-script knob). The run prefix is detected from
	// the lockfile, mirroring the circleci generator's package-manager probe.
	if config.Language == "node" {
		p.NodeDevLintHook = true
		p.NodeRunPrefix = nodeRunPrefix(workingDir)
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
		inputs = append(inputs, file.NewCreateAppPlatformValuesInput(p.params, chartName))
	}
	return inputs
}

// CreateValuesSchemaInputs generates helm/<chart>/values.schema.json itself, in-process,
// for every discovered chart. It must run after CreateSchemaYamlInputs' inputs have been
// written to disk: generation reads helm/<chart>/values.yaml and the generated
// zz_generated.app-platform.values.yaml back off disk. See giantswarm/devctl#2195.
func (p *PreCommit) CreateValuesSchemaInputs() []input.Input {
	var inputs []input.Input
	for _, chartName := range p.params.HelmCharts {
		inputs = append(inputs, file.NewCreateValuesSchemaInput(p.params, chartName))
	}
	return inputs
}

// pinnedVersions reads devctl's own module dependency graph to find the exact versions of
// the two libraries the generated hook pins via additional_dependencies, so go.mod stays
// the single source of truth instead of hardcoding them a second time in the template.
func pinnedVersions() (helmValuesSchemaJSONVersion, schemalintVersion string) {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range bi.Deps {
			switch dep.Path {
			case helmValuesSchemaJSONModule:
				helmValuesSchemaJSONVersion = dep.Version
			case schemalintModule:
				schemalintVersion = dep.Version
			}
		}
	}
	if helmValuesSchemaJSONVersion != "" && schemalintVersion != "" {
		return helmValuesSchemaJSONVersion, schemalintVersion
	}

	// ponytail: debug.ReadBuildInfo().Deps is only populated for a real `go build` binary
	// -- a `go test` binary always reports it empty -- so tests would otherwise never see
	// a version. Fall back to reading go.mod straight off disk, found by walking up from
	// this very source file. Ceiling: that only resolves while the source tree is on disk
	// (`go test`, `go run`). Released binaries are built with -trimpath
	// (Makefile.gen.go.mk), which rewrites runtime.Caller(0) to a path that exists
	// nowhere, so there the fallback returns empty and New() fails with the clear error
	// above -- build info, not this, is what covers a released binary.
	return pinnedVersionsFromGoMod()
}

func pinnedVersionsFromGoMod() (helmValuesSchemaJSONVersion, schemalintVersion string) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", ""
	}

	for dir := filepath.Dir(thisFile); ; {
		content, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			mf, err := modfile.Parse("go.mod", content, nil)
			if err != nil {
				return "", ""
			}
			for _, req := range mf.Require {
				switch req.Mod.Path {
				case helmValuesSchemaJSONModule:
					helmValuesSchemaJSONVersion = req.Mod.Version
				case schemalintModule:
					schemalintVersion = req.Mod.Version
				}
			}
			return helmValuesSchemaJSONVersion, schemalintVersion
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

func (p *PreCommit) CreateHelmReadmeInputs() []input.Input {
	var inputs []input.Input
	for _, chartName := range p.params.HelmCharts {
		inputs = append(inputs, file.NewCreateHelmReadmeInput(p.params, chartName))
	}
	return inputs
}
