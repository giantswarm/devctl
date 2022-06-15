package cmd

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	flagNoCache = "no-cache"
)

type flag struct {
	NoCache  bool
	LogLevel string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.NoCache, flagNoCache, false, "Disable version cache.")
	cmd.PersistentFlags().StringVar(&f.LogLevel, "log-level", logrus.InfoLevel.String(), "logging level")
}

func (f *flag) Validate() error {
	return nil
}
