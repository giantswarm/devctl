package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/termout"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// exitError is the exit code of a command that failed.
const exitError = 2

// Execute runs root with the process arguments and returns the exit code.
// An agent-facing command has written its JSON document already: its
// outcome is the exit code and nothing more is printed. Any other error is
// printed on stderr for a person: a wrong call with the command's usage line
// and a pointer to its help, the stack trace only at --log-level debug. An
// error that knows its exit code ([agentcli.ExitCoder], such as
// [authstore.ErrAuthRequired], exit 8) exits with it; any other exits 2.
func Execute(root *cobra.Command, stderr io.Writer) int {
	return ExecuteThen(root, stderr, nil)
}

// ExecuteThen is [Execute], and calls after with the command that ran, once
// it ran and whatever its outcome, before returning the exit code. after
// cannot change the exit code.
func ExecuteThen(root *cobra.Command, stderr io.Writer, after func(executed *cobra.Command)) int {
	executed, err := root.ExecuteC()
	if after != nil {
		defer after(executed)
	}
	if err == nil {
		return 0
	}

	var exitErr *agentcli.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	fmt.Fprintf(stderr, "%s: %s\n", termout.Error(stderr), prettyError(err, debug(root)))

	var coder agentcli.ExitCoder
	var usage *UsageError
	switch {
	case errors.As(err, &coder):
		return coder.ExitCode()
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

// prettyError keeps the full message of a joined error tree. microerror.Pretty
// selects only the first annotation, losing other failures and their context.
// Single errors retain their existing formatting, and debug retains the trace.
func prettyError(err error, stackTrace bool) string {
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if _, joined := cause.(interface{ Unwrap() []error }); joined {
			message := err.Error()
			if stackTrace {
				message += strings.TrimPrefix(microerror.Pretty(err, true), microerror.Pretty(err, false))
			}
			return message
		}
	}
	return microerror.Pretty(err, stackTrace)
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
