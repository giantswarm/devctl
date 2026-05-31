package circleci

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "circleci"
	shortDescription = `Generates a .circleci/config.yml for giantswarm projects.`
	longDescription  = `Generates a .circleci/config.yml emitting the aligned giantswarm standard.

The pipeline is fully derived from existing signals -- there is no CI parameter
block. Jobs are selected by:

  - language go        -> architect/go-build
  - Dockerfile present -> architect/push-to-registries (multiarch + split-china-push)
                          and architect/sync-china-registry
  - app flavour        -> architect/push-to-app-catalog (app-build-suite executor)
                          and architect/run-tests-with-ats

The orb is pinned to the aligned standard and tag/branch filters follow the
giantswarm convention (branch builds amd64-only, tags build multi-arch and
publish the chart).`
	example = `  devctl gen circleci --repo-name mcp-kubernetes --language go --flavour app
  devctl gen circleci --repo-name crd-docs-generator --language go`
)

type Config struct {
	Logger micrologger.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	f := &flag{}

	r := &runner{
		flag:   f,
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:     name,
		Short:   shortDescription,
		Long:    longDescription,
		Example: example,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
