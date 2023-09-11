package ami

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	flagArch                    = "arch"
	flagChannel                 = "stable"
	flagChinaBucketName         = "china.bucket"
	flagChinaBucketRegion       = "china.region"
	flagChinaAWSAccessKeyID     = "aws.accesskeyid"
	flagChinaAWSSecretAccessKey = "aws.secretaccesskey"
	flagDir                     = "dir"
	flagMinimumVersion          = "minimum.version"
	flagPrimaryDomain           = "primary.domain"
	flagKeepExisting            = "keep.existing"
)

type flag struct {
	Arch                    string
	Channel                 string
	ChinaBucketName         string
	ChinaBucketRegion       string
	ChinaAWSAccessKeyID     string
	ChinaAWSSecretAccessKey string
	Dir                     string
	MinimumVersion          string
	PrimaryDomain           string
	KeepExisting            string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Arch, flagArch, "amd64-usr", `Architecture of the image, e.g. "amd64-usr".`)
	cmd.Flags().StringVar(&f.Channel, flagChannel, "stable", `Channel of the OS.`)
	cmd.Flags().StringVar(&f.ChinaBucketName, flagChinaBucketName, "flatcar-prod-ami-import-cn-north-1", `S3 bucket name to get version info in china.`)
	cmd.Flags().StringVar(&f.ChinaBucketRegion, flagChinaBucketRegion, "cn-north-1", `Region containing S3 bucket to get version info in china.`)
	cmd.Flags().StringVar(&f.ChinaAWSAccessKeyID, flagChinaAWSAccessKeyID, "", `AWS Access Key ID for china.`)
	cmd.Flags().StringVar(&f.ChinaAWSSecretAccessKey, flagChinaAWSSecretAccessKey, "", `AWS Secret Access Key for china.`)
	cmd.Flags().StringVar(&f.Dir, flagDir, "", `Directory/package where the generated code should be located.`)
	cmd.Flags().StringVar(&f.MinimumVersion, flagMinimumVersion, "2191.5.0", `Minimum version of flatcar to use for generation, e.g. "2134.3.0".`)
	cmd.Flags().StringVar(&f.PrimaryDomain, flagPrimaryDomain, "flatcar-linux.net", `Domain to use as a source for AMIs.`)
	cmd.Flags().StringVar(&f.KeepExisting, flagKeepExisting, "", `Keep versions already defined in file specified.`)

}

func (f *flag) Validate() error {
	if f.Dir == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDir)
	}

	return nil
}
