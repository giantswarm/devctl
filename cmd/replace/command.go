package replace

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	usage       = "replace [flags] [PATTERN] [REPLACEMENT] [GLOB ...]"
	description = `Replaces text in files. PATTERN is Go regular expressions and REPLACEMENT is Go regular expressions replacement string. GLOB is a file path pattern recognizing "*", "**" and "?" globing.`
	example     = `  devctl replace foo bar /path/to/file
  devctl replace -i '^(\\w+).+' '$1 foobar' /file/a /file/b
  devctl replace -i --ignore='**/*.yaml' 'a' 'b' /dir/*
  devctl replace -i 'a' 'b' /dir/**/*.go main.go`
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
		Use:     usage,
		Example: example,
		Short:   description,
		Long:    description,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
