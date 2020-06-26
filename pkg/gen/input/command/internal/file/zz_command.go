package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/params"
)

func NewZZCommandInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "command.go"),
		TemplateBody: zzCommandTemplate,
		TemplateData: map[string]interface{}{
			"Package": params.Package(p),
			"Name":    params.Name(p),
		},
	}

	return i
}

var zzCommandTemplate = `// DO NOT EDIT. Generated with:
//
//	devctl gen command
//
package {{ .Package }}

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/{{ .Name }}/pkg/project"
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
		flags:  flags{},
		logger: config.Logger,
	}

	var name string
	var use string
	{
		pkg := reflect.TypeOf(Config{}).PkgPath()
		split := strings.Split(pkg, "/")
		if len(split) < 4 {
			panic(fmt.Sprintf("expected at least 4 segments in package name %q but got %d", pkg, len(split)))
		}
		segments := append([]string{project.Name()}, split[4:]...)
		use = strings.Join(segments, " ")
		name = segments[len(segments)-1]
	}

	var examplesStr string
	if len(examples) > 0 {
		var b strings.Builder
		for i, e := range examples {
			if i != 0 {
				b.WriteString("\n")
			}
			b.WriteString("  ")
			isComment := strings.HasPrefix(e, "#")
			if !isComment {
				b.WriteString(use)
				b.WriteString(" ")
			}
			b.WriteString(e)
			if !isComment {
				b.WriteString("\n")
			}
		}

		examplesStr = b.String()
	}

	c := &cobra.Command{
		Use:          name,
		Example:      examplesStr,
		Short:        description,
		Long:         description,
		SilenceUsage: true,
		RunE:         r.Run,
	}

	c.SetOut(config.Stdout)
	c.SetErr(config.Stderr)

	initFlags(c, &r.flags)

	return c, nil
}
`
