package circleci

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci/internal/file"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci/internal/params"
)

// DefaultOrbVersion is the aligned giantswarm/architect orb version every
// generated CircleCI config pins. Bumping the org-wide standard is a one-line
// change here.
const DefaultOrbVersion = "9.0.0"

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
	// OrbVersion overrides DefaultOrbVersion when set.
	OrbVersion string
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

	orbVersion := config.OrbVersion
	if orbVersion == "" {
		orbVersion = DefaultOrbVersion
	}

	c := &CircleCI{
		params: params.Params{
			RepoName:      config.RepoName,
			Language:      config.Language.String(),
			HasDockerfile: config.HasDockerfile,
			HasApp:        hasApp,
			OrbVersion:    orbVersion,
		},
	}

	return c, nil
}

func (c *CircleCI) Config() input.Input {
	return file.NewConfigInput(c.params)
}
