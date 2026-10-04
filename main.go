package main

import (
	"context"
	"fmt"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
)

func main() {
	// Started as a proxy (a `gh` link to devctl), devctl runs that program
	// with the App login's token: `devctl auth exec`.
	if name := authexec.Proxy(os.Args[0]); name != "" {
		os.Exit(authexec.Run(context.Background(), authexec.Default(), name, os.Args[1:]))
	}

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
