package circleci

import (
	"context"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, _ *cobra.Command, _ []string) error {
	var err error

	// The image pipeline is derived from repo content: architect already
	// requires a Dockerfile to build an image, so its presence is the signal.
	_, statErr := os.Stat("Dockerfile")
	hasDockerfile := statErr == nil

	// Node package manager is derived from the lockfile, the same content-signal
	// style as the Dockerfile probe. An explicit --package-manager wins.
	packageManager := r.flag.PackageManager
	if packageManager == "" && r.flag.Language == gen.LanguageNode {
		packageManager = detectPackageManager()
	}

	// Node version is derived from .nvmrc, the same content-signal style. An
	// explicit --node-image-version wins; neither set falls back to devctl's
	// baked-in default.
	nodeImageVersion := r.flag.NodeImageVersion
	if nodeImageVersion == "" && r.flag.Language == gen.LanguageNode {
		nodeImageVersion = detectNodeVersion()
	}

	var circleciInput *circleci.CircleCI
	{
		c := circleci.Config{
			RepoName:         r.flag.RepoName,
			Language:         r.flag.Language,
			Flavours:         r.flag.Flavours,
			SkipATS:          r.flag.SkipATS,
			HasDockerfile:    hasDockerfile,
			AppCatalog:       r.flag.AppCatalog,
			AppCatalogTest:   r.flag.AppCatalogTest,
			ChartName:        r.flag.ChartName,
			ForcePublic:      r.flag.ForcePublic,
			BranchPublish:    r.flag.BranchPublish,
			BuildConcurrency: r.flag.BuildConcurrency,
			ImagePreBuildJob: r.flag.ImagePreBuildJob,
			ImagePrivateOnly: r.flag.ImagePrivateOnly,
			ImageName:        r.flag.ImageName,
			ImagePlatforms:   r.flag.ImagePlatforms,
			ImageDockerfile:  r.flag.ImageDockerfile,
			ResourceClass:    r.flag.ResourceClass,
			PackageManager:   packageManager,
			NodeImageVersion: nodeImageVersion,
			NodeTestTarget:   r.flag.NodeTestTarget,
			NodeBuildTarget:  r.flag.NodeBuildTarget,
			NodeBuildOutput:  r.flag.NodeBuildOutput,
		}

		circleciInput, err = circleci.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	inputs := []input.Input{
		circleciInput.SetupConfig(),
		circleciInput.Workflows(),
	}
	// The canonical ATS Pipfile rides on the same chart/app (.HasApp) signal
	// that emits the run-tests-with-ats jobs, so it is folded into this
	// generator rather than invoked separately (which would risk leaking it
	// into non-generated-CI repos). ATSInputs returns nil for non-app repos.
	inputs = append(inputs, circleciInput.ATSInputs()...)

	err = gen.Execute(ctx, inputs...)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// detectPackageManager picks the Node package manager from the lockfile present
// in the working directory, mirroring the Dockerfile content-probe. npm and
// pnpm are unambiguous by lockfile name; a yarn.lock is Classic only if it
// carries the v1 header comment, otherwise it is Berry (the empty default).
func detectPackageManager() string {
	if _, err := os.Stat("package-lock.json"); err == nil {
		return circleci.PackageManagerNPM
	}
	if _, err := os.Stat("pnpm-lock.yaml"); err == nil {
		return circleci.PackageManagerPNPM
	}
	if data, err := os.ReadFile("yarn.lock"); err == nil {
		if strings.Contains(string(data), "yarn lockfile v1") {
			return circleci.PackageManagerYarnClassic
		}
		return circleci.PackageManagerYarn
	}

	return ""
}

// detectNodeVersion reads the repo's .nvmrc, mirroring the lockfile probe. It
// is the opt-in that lets a repo own its Node version in ONE place: the same
// file drives local dev (nvm/fnm/asdf/volta), actions/setup-node via
// node-version-file, and -- through this probe -- the generated cimg/node tag
// and the node-build cache-key salt, which are otherwise the copies most prone
// to silent drift.
//
// Only an exact version is honoured. .nvmrc also accepts aliases ("lts/*",
// "node") and partial versions ("24"), none of which name a cimg/node tag; a
// repo using one of those keeps devctl's baked-in default rather than
// rendering an image that does not exist. Comments and surrounding whitespace
// are stripped, matching how nvm and Renovate's nvm manager read the file.
func detectNodeVersion() string {
	data, err := os.ReadFile(".nvmrc")
	if err != nil {
		return ""
	}

	for line := range strings.Lines(string(data)) {
		version, _, _ := strings.Cut(line, "#")
		version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "v"))
		if version == "" {
			continue
		}
		if !exactNodeVersionRE.MatchString(version) {
			return ""
		}
		return version
	}

	return ""
}

// exactNodeVersionRE matches a fully-qualified Node version (major.minor.patch)
// -- the only form that maps 1:1 onto a cimg/node tag.
var exactNodeVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
