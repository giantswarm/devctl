package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/params"
)

func NewFlagsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.FileName(p, "flags.go"),
		TemplateBody: flagsTemplate,
		TemplateData: map[string]interface{}{
			"Grave":   "`",
			"Package": params.Package(p),
		},
	}

	return i
}

var flagsTemplate = `package {{ .Package }}

import (
	"context"

	flag "github.com/spf13/pflag"
)

const (
//	flagExample           = "example"
//	flagExamplePersistent = "example-persistent"
)

type localFlags struct {
//	Example string
}

type persistentFlags struct {
//	ExamplePersistent string
}

func initLocalFlags(f *flag.FlagSet, flags *localFlags) {
//	f.StringVarP(&flags.Example, flagExample, "e", "", {{ .Grave }}Example flag that has to be non empty.{{ .Grave }})
}

func initPersistentFlags(f *flag.FlagSet, flags *persistentFlags) {
//	f.StringVarP(&flags.ExamplePersistent, flagExamplePersistent, "p", "", {{ .Grave }}Example persistent flag that has to be non empty.{{ .Grave }})
}

func validateLocalFlags(ctx context.Context, flags localFlags) error {
//	if flags.Example == "" {
//		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagExample)
//	}

	return nil
}

func validatePersistentFlags(ctx context.Context, flags persistentFlags) error {
//	if flags.ExamplePersistent == "" {
//		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagExamplePersistent)
//	}

	return nil
}
`
