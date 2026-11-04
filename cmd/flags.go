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
	f.LogLevel = logrus.InfoLevel.String()
	cmd.PersistentFlags().Var((*logLevelValue)(&f.LogLevel), flagLogLevel, "Logging level; debug also prints the stack trace of an error.")
}

// logLevelValue validates during flag parsing so a bad level reaches the
// command's flag-error handler, including an agent command's JSON document.
type logLevelValue string

func (v *logLevelValue) String() string { return string(*v) }

func (v *logLevelValue) Type() string { return "string" }

func (v *logLevelValue) Set(value string) error {
	if _, err := logrus.ParseLevel(value); err != nil {
		return err
	}
	*v = logLevelValue(value)
	return nil
}

func (f *flag) Validate() error {
	return nil
}
