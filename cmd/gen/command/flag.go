package command

import (
	"bytes"
	"os/exec"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagDir      = "dir"
	flagGoModule = "go-module"
)

type flag struct {
	Dir      string
	GoModule string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Dir, flagDir, "d", "", `Relative command directory/package. Must start with "cmd".`)
	cmd.Flags().StringVarP(&f.GoModule, flagGoModule, "m", initDefaultGoModule(), `Go module name.`)
}

func (f *flag) Validate() error {
	if f.Dir == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDir)
	}
	if f.GoModule == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagGoModule)
	}

	if f.Dir != "cmd" && !strings.HasPrefix(f.Dir, "cmd/") {
		return microerror.Maskf(invalidFlagError, "--%s must value must start with %q", flagDir, "cmd")
	}

	return nil
}

func initDefaultGoModule() string {
	out, _ := exec.Command("go", "list", ".").Output()
	out = bytes.TrimSpace(out)
	return string(out)
}
