package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"text/template"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
)

func Execute(ctx context.Context, w io.Writer, f input.Input) error {
	var err error

	tmpl := template.New(fmt.Sprintf("%T", f))

	emptyDelims := input.InputTemplateDelims{}
	if f.TemplateDelims != emptyDelims {
		tmpl = tmpl.Delims(f.TemplateDelims.Left, f.TemplateDelims.Right)
	}

	if len(f.TemplateFuncs) > 0 {
		tmpl.Funcs(f.TemplateFuncs)
	}

	tmpl, err = tmpl.Parse(f.TemplateBody)
	if err != nil {
		return microerror.Mask(err)
	}

	pr, pw := io.Pipe()

	err = tmpl.Execute(pw, f.TemplateData)
	if err != nil {
		return microerror.Mask(err)
	}

	if input.PostProcessGoFmt {
		err := goFmt(ctx, pr, w)
		if err != nil {
			return microerror.Mask(err)
		}
	} else {
		_, err := io.Copy(w, pr)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}

func goFmt(ctx context.Context, code io.Reader, out io.Writer) error {
	cmd := exec.Command("go", "fmt")
	cmd.Stdin = code
	cmd.Stdout = out
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		var exitErr *exec.ExitError
		if errors.As(err, exitErr) {
			stderr = "\n\n" + string(exitErr.Stderr)
		}
		return microerror.Maskf(executionFailedError, "failed to run %q with error %#q%s", cmd.Path+" "+strings.Join(cmd.Args, " "), err, stderr)
	}

	return nil
}
