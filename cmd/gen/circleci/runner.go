package circleci

import (
	"context"
	"fmt"
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
		var rejected string
		nodeImageVersion, rejected = detectNodeVersion()
		// A .nvmrc that names no cimg/node tag is the one case worth saying out
		// loud: the repo asked for a Node version and did not get it, and the
		// only visible symptom would be an unchanged workflows.yml.
		if rejected != "" {
			fmt.Fprintf(r.stderr, "warning: ignoring .nvmrc value %q -- the Node job needs an exact major.minor.patch (e.g. 24.19.0); falling back to %s\n", rejected, circleci.DefaultNodeImageVersion)
		}
	}

	var circleciInput *circleci.CircleCI
	{
		c := circleci.Config{
			RepoName:         r.flag.RepoName,
			Language:         r.flag.Language,
			Flavours:         r.flag.Flavours,
			SkipAppCatalog:   r.flag.SkipAppCatalog,
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
// Only an exact major.minor.patch is honoured, and deliberately so. .nvmrc also
// accepts aliases ("lts/*", "node") and partial versions; of those only a bare
// major names no cimg/node tag at all, since cimg does publish a floating
// major.minor (cimg/node:24.19 exists). Accepting major.minor would still
// defeat the point: the repo's Dockerfile FROM pins an exact patch, so a
// floating .nvmrc would drift from it -- reintroducing exactly the divergence
// this probe removes -- and it would coarsen the node-build cache-key salt,
// which exists to be exact. A repo using an unusable form therefore keeps
// devctl's baked-in default, and the caller says so out loud (see the second
// return value): silently falling back is what would make CI run a different
// Node than local dev without anyone noticing.
//
// Comments and surrounding whitespace are stripped, matching how nvm and
// Renovate's nvm manager read the file.
//
// The second return value is the raw .nvmrc value when the file exists but
// names no usable version -- empty both when there is no .nvmrc (the expected
// case, no warning warranted) and when a version was parsed.
func detectNodeVersion() (version, rejected string) {
	data, err := os.ReadFile(".nvmrc")
	if err != nil {
		return "", ""
	}

	for line := range strings.Lines(string(data)) {
		value, _, _ := strings.Cut(line, "#")
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
		if value == "" {
			continue
		}
		if !exactNodeVersionRE.MatchString(value) {
			return "", value
		}
		return value, ""
	}

	return "", ""
}

// exactNodeVersionRE matches a fully-qualified Node version (major.minor.patch).
// See detectNodeVersion for why the less specific forms cimg does publish are
// still rejected.
var exactNodeVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
