package project

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
)

const (
	flagFlavour  = "flavour"
	flagGoModule = "go-module"
)

type flag struct {
	Flavour  string
	GoModule string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Flavour, flagFlavour, "f", "", fmt.Sprintf(`The type of project that you want to generate the Makefile for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().StringVarP(&f.GoModule, flagGoModule, "m", initDefaultGoModule(), `Go module name.`)
}

func (f *flag) Validate() error {
	if !gen.IsValidFlavour(f.Flavour) {
		return microerror.Maskf(invalidFlagError, "--%s must be one of <%s>", flagFlavour, strings.Join(gen.AllFlavours(), "|"))
	}
	if f.GoModule == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagGoModule)
	}

	return nil
}

func initDefaultGoModule() string {
	out, _ := exec.Command("go", "list", ".").Output()
	out = bytes.TrimSpace(out)
	return string(out)
}
