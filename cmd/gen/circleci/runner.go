package circleci

import (
	"context"
	"io"
	"os"
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

	var circleciInput *circleci.CircleCI
	{
		c := circleci.Config{
			RepoName:         r.flag.RepoName,
			Language:         r.flag.Language,
			Flavours:         r.flag.Flavours,
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
