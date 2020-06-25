package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/params"
)

func NewFlagsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "flags.go"),
		TemplateBody: flagsTemplate,
		TemplateData: map[string]interface{}{
			"IsRoot":  params.IsRoot(p),
			"Name":    params.Name(p),
			"Package": params.Package(p),
			"Parent":  params.Parent(p),
		},
	}

	return i
}

var flagsTemplate = `// DO NOT EDIT. Generated with:
//
//	devctl gen command
//
// If you need to add your own error please create error.go file next to this
// file.
package {{ .Package }}

import (
	"context"

	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"

	"github.com/giantswarm/microerror"

	{{- if not .IsRoot }}

	"github.com/giantswarm/{{ .Name }}/{{ .Parent }}"

	{{- end }}
)

type PersistentFlags struct {
	{{- if not .IsRoot }}
	{{ .Parent }}.PersistentFlags
	{{- end }}
	persistentFlags
}

func InitPersistentFlags(flag *flag.FlagSet, flags *PersistentFlags) {
	{{- if not .IsRoot }}
	{{ .Parent }}.InitPersistentFlags(flag, &flags.PersistentFlags)
	{{- end }}
	initPersistentFlags(flag, &flags.persistentFlags)
}

func ValidatePersistentFlags(ctx context.Context, flags PersistentFlags) error {
	var err error

	{{- if not .IsRoot }}

	err = {{ .Parent }}.ValidatePersistentFlags(ctx, flags.PersistentFlags)
	if err != nil {
		return microerror.Mask(err)
	}

	{{- end }}

	err = validatePersistentFlags(ctx, flags.persistentFlags)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

type flags struct {
	PersistentFlags
	localFlags
}

func initFlags(cmd *cobra.Command, flags *flags) {
	InitPersistentFlags(cmd.PersistentFlags(), &flags.PersistentFlags)
	initLocalFlags(cmd.Flags(), &flags.localFlags)
}

func validateFlags(ctx context.Context, flags flags) error {
	var err error

	err = ValidatePersistentFlags(ctx, flags.PersistentFlags)
	if err != nil {
		return microerror.Mask(err)
	}

	err = validateLocalFlags(ctx, flags.localFlags)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
`
