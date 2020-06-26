package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/mainpkg/internal/params"
)

func NewZZMainInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "main.go"),
		TemplateBody: mainTemplate,
		TemplateData: map[string]interface{}{
			"Subcommands": params.Subcommands(p),
		},
	}

	return i
}

var mainTemplate = `package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"

	cmd "github.com/giantswarm/mycli/cmd"
{{- range .Subcommands }}
	{{ .Alias }} "{{ .Import }}"
{{- end }}
)

func main() {
	var uerr *microerror.Error
	err := mainE(context.Background())
	if errors.As(err, &uerr) && uerr.Kind == "invalidFlagError" {
		// For invalid flag error printing stack creates only noise.
		os.Exit(2)
	} else if err != nil {
		panic(fmt.Sprintf("%#v\n", err))
	}
}

func mainE(ctx context.Context) error {
	config, err := newMainConfig()
	if err != nil {
		return microerror.Mask(err)
	}

	rootCmd, err := cmd.New(cmd.Config(config))
	if err != nil {
		return microerror.Mask(err)
	}
{{- range .Subcommands }}

	{{ .Alias }}Cmd, err := {{ .Alias }}.New({{ .Alias }}.Config(config))
	if err != nil {
		return microerror.Mask(err)
	}
	{{ .ParentAlias }}Cmd.AddCommand({{ .Alias }}Cmd)
{{- end }}

	err = rootCmd.Execute()
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

type mainConfig struct {
	Logger micrologger.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func newMainConfig() (mainConfig, error) {
	var err error

	var logger micrologger.Logger
	{
		c := micrologger.Config{}

		logger, err = micrologger.New(c)
		if err != nil {
			return mainConfig{}, microerror.Mask(err)
		}
	}

	c := mainConfig{
		Logger: logger,
		Stderr: os.Stderr,
		Stdout: os.Stdout,
	}

	return c, nil
}
`
