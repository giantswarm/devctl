package cmd

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	flagLogLevel = "log-level"
	flagNoCache  = "no-cache"
)

type flag struct {
	NoCache  bool
	LogLevel string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.NoCache, flagNoCache, false, "Disable version cache.")
	cmd.PersistentFlags().StringVar(&f.LogLevel, flagLogLevel, logrus.InfoLevel.String(), "Logging level; debug also prints the stack trace of an error.")
}

func (f *flag) Validate() error {
	return nil
}
