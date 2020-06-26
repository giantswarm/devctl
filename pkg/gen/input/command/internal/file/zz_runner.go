package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/params"
)

func NewZZRunnerInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "runner.go"),
		TemplateBody: zzRunnerTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(p),
		},
	}

	return i
}

var zzRunnerTemplate = `// DO NOT EDIT. Generated with:
//
//	devctl gen command
//
package {{ .Package }}

import (
	"context"
	"fmt"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

type runner struct {
	cmd    *cobra.Command
	flags  flags
	logger micrologger.Logger
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	r.cmd = cmd

	err := validateFlags(ctx, r.flags)
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) Print(i ...interface{}) {
	r.cmd.Print(i...)
}

func (r *runner) PrintErr(i ...interface{}) {
	r.cmd.PrintErr(i...)
}

func (r *runner) PrintErrf(format string, i ...interface{}) {
	r.PrintErr(fmt.Sprintf(format, i...))
}

func (r *runner) Printf(format string, i ...interface{}) {
	r.Print(fmt.Sprintf(format, i...))
}
`
