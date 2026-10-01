package main

import (
	"fmt"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd"
)

func main() {
	rootCommand, err := newRootCommand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", microerror.Pretty(err, true))
		os.Exit(2)
	}

	os.Exit(cmd.Execute(rootCommand, os.Stderr))
}

func newRootCommand() (*cobra.Command, error) {
	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	rootCommand, err := cmd.New(cmd.Config{Logger: logger})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return rootCommand, nil
}
