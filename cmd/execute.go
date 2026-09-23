package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// exitError is the exit code of a command that failed.
const exitError = 2

// Execute runs root with the process arguments and returns the exit code.
// An agent-facing command has written its JSON document already: its
// outcome is the exit code and nothing more is printed. Any other error is
// printed on stderr for a person: a wrong call with the command's usage line
// and a pointer to its help, the stack trace only at --log-level debug.
func Execute(root *cobra.Command, stderr io.Writer) int {
	executed, err := root.ExecuteC()
	if err == nil {
		return 0
	}

	var exitErr *agentcli.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	fmt.Fprintf(stderr, "Error: %s\n", microerror.Pretty(err, debug(root)))

	var usage *UsageError
	switch {
	case errors.As(err, &usage):
		executed = usage.Command
		if usage.Usage {
			fmt.Fprintf(stderr, "Usage: %s\n", executed.UseLine())
		}
	case isUsageKind(err):
		fmt.Fprintf(stderr, "Usage: %s\n", executed.UseLine())
	default:
		return exitError
	}
	fmt.Fprintf(stderr, "Run '%s --help' for more information.\n", executed.CommandPath())

	return exitError
}

// debug reports whether --log-level asks for debug output or more.
func debug(root *cobra.Command) bool {
	flag := root.PersistentFlags().Lookup(flagLogLevel)
	if flag == nil {
		return false
	}
	level, err := logrus.ParseLevel(flag.Value.String())
	return err == nil && level >= logrus.DebugLevel
}
