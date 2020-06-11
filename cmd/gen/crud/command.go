package crud

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name        = "crud"
	description = "Generates legacy CRUD interface files for operator projects."
	example     = `
    Migrating a handler using the legacy CRUD interface can be done using the following
    script. Note that the script must be executed within the handler package you want to
    refactor. Note that the script does not fix the handler wiring in the controller file,
    which still needs to be done manually.

    $ cat crud.sh
    #!/bin/bash

    echo "fixing existing code"
    devctl replace -i 'ApplyCreateChange' 'applyCreateChange' . > /dev/null 2>&1
    devctl replace -i 'ApplyDeleteChange' 'applyDeleteChange' . > /dev/null 2>&1
    devctl replace -i 'ApplyUpdateChange' 'applyUpdateChange' . > /dev/null 2>&1
    devctl replace -i 'GetCurrentState' 'getCurrentState' . > /dev/null 2>&1
    devctl replace -i 'GetDesiredState' 'getDesiredState' . > /dev/null 2>&1
    devctl replace -i 'NewDeletePatch' 'newDeletePatch' . > /dev/null 2>&1
    devctl replace -i 'NewUpdatePatch' 'newUpdatePatch' . > /dev/null 2>&1
    devctl replace -i ' Patch ' ' patch ' . > /dev/null 2>&1
    devctl replace -i 'crud.Patch' 'patch' . > /dev/null 2>&1
    devctl replace -i 'crud.NewPatch' 'newPatch' . > /dev/null 2>&1

    echo "generating new code"
    devctl gen crud

    echo "formatting all code"
    gofmt -l -s -w .
    goimports -l -w .`
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

	r := &runner{
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:     name,
		Short:   description,
		Long:    description,
		Example: example,
		RunE:    r.Run,
	}

	return c, nil
}
