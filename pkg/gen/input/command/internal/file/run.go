package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/params"
)

func NewRunInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.FileName(p, "run.go"),
		TemplateBody: runTemplate,
		TemplateData: map[string]interface{}{
			"Grave":   "`",
			"Package": params.Package(p),
		},
	}

	return i
}

var runTemplate = `package {{ .Package }}

import (
	"context"
)

func (r *runner) run(ctx context.Context, args []string) error {
	r.Printf("\t%-25s=  %s\n", "command", r.cmd.Use)
	r.Printf("\t%-25s=  %v\n", "args", args)
	//r.Printf("\t--%-23s=  %s\n", flagExample, r.flags.Example)
	//r.Printf("\t--%-23s=  %s\n", flagExamplePersistent, r.flags.ExamplePersistent)
	r.Printf("\t%-25s=  %+v\n", "flags", r.flags)

	//err := r.cmd.Usage()
	//if err != nil {
	//	return microerror.Mask(err)
	//}

	return nil
}
`
