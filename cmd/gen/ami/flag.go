package ami

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	flagArch           = "arch"
	flagChannel        = "stable"
	flagChinaDomain    = "china.domain"
	flagDir            = "dir"
	flagMinimumVersion = "minimum.version"
	flagPrimaryDomain  = "primary.domain"
)

type flag struct {
	Arch           string
	Channel        string
	ChinaDomain    string
	Dir            string
	MinimumVersion string
	PrimaryDomain  string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Arch, flagArch, "amd64-usr", `Architecture of the image, e.g. "amd64-usr".`)
	cmd.Flags().StringVar(&f.Channel, flagChannel, "stable", `Channel of the OS.`)
	cmd.Flags().StringVar(&f.ChinaDomain, flagChinaDomain, "flatcar-prod-ami-import-cn-north-1.s3.cn-north-1.amazonaws.com.cn", `Domain to use as a source for AMIs in china.`)
	cmd.Flags().StringVar(&f.Dir, flagDir, "", `Directory/package where the generated code should be located.`)
	cmd.Flags().StringVar(&f.MinimumVersion, flagMinimumVersion, "2191.5.0", `Minimum version of flatcar to use for generation, e.g. "2134.3.0".`)
	cmd.Flags().StringVar(&f.PrimaryDomain, flagPrimaryDomain, "flatcar-linux.net", `Domain to use as a source for AMIs.`)
}

func (f *flag) Validate() error {
	if f.Dir == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDir)
	}

	return nil
}
