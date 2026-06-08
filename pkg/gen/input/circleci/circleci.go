package circleci

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci/internal/file"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci/internal/params"
)

// OrbVersion is the aligned giantswarm/architect orb version every generated
// CircleCI config pins. It is baked in next to the template -- not a flag and
// not passed in by callers -- so that an orb bump (which can change the
// template's required job/param shape, i.e. a cross-major compatibility
// contract) forces a new devctl release rather than silently combining a stale
// template with a newer orb at generation time.
//
// Renovate keeps this current; a major bump lands as a devctl PR, gets released,
// and only then reaches repos via the align-files devctl pin.
//
// renovate: datasource=orb depName=giantswarm/architect
const OrbVersion = "9.1.0"

type Config struct {
	// RepoName is the repository name, used for the binary, chart, and job
	// names.
	RepoName string
	// Language is the repo language. "go" selects the go-build job.
	Language gen.Language
	// Flavours are the devctl gen flavours. The "app" flavour selects the
	// chart pipeline.
	Flavours gen.FlavourSlice
	// HasDockerfile selects the image pipeline. The runner derives this from
	// the presence of a Dockerfile in the repo.
	HasDockerfile bool
	// BranchPublish opts the repo into publishing a dev image and chart on
	// branch builds. By default branches build + test only (no push). When
	// set, the branch path additionally pushes an amd64 dev image and the
	// dev chart, coupled (both or neither).
	BranchPublish bool
}

// shipsBinaries reports whether the repo distributes cross-platform Go binaries
// on its GitHub Release. The "cli" flavour is the signal: it marks a repo whose
// users download a binary, as opposed to a chart-wrapped service or operator.
// Requires Go -- the binary comes from go-build.
func (c Config) shipsBinaries() bool {
	return c.Language == gen.LanguageGo && c.Flavours.Contains(gen.FlavourCLI)
}

type CircleCI struct {
	params params.Params
}

func New(config Config) (*CircleCI, error) {
	// Every job is derived from a signal. With none of them set the template
	// renders an empty `jobs:` list, which is an invalid CircleCI config.
	hasApp := config.Flavours.Contains(gen.FlavourApp)
	if config.Language != gen.LanguageGo && !config.HasDockerfile && !hasApp {
		return nil, microerror.Maskf(invalidConfigError, "no jobs would be generated: set --language=go, add a Dockerfile, or use the app flavour")
	}

	c := &CircleCI{
		params: params.Params{
			RepoName:        config.RepoName,
			Language:        config.Language.String(),
			HasDockerfile:   config.HasDockerfile,
			HasApp:          hasApp,
			BranchPublish:   config.BranchPublish,
			ReleaseBinaries: config.shipsBinaries(),
			OrbVersion:      OrbVersion,
		},
	}

	return c, nil
}

func (c *CircleCI) Config() input.Input {
	return file.NewConfigInput(c.params)
}
