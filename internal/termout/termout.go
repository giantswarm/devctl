// Package termout colours the words devctl prefixes its messages with, and
// reads the global --log-level flag, for the commands that print on stderr.
package termout

import (
	"io"
	"os"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// Error is "Error", red when w is a terminal.
func Error(w io.Writer) string { return paint(w, "Error", color.FgRed) }

// Warning is "Warning", yellow when w is a terminal.
func Warning(w io.Writer) string { return paint(w, "Warning", color.FgYellow) }

// Debug reports whether --log-level on cmd (or a parent) is debug or more.
func Debug(cmd *cobra.Command) bool {
	flag := cmd.Flag("log-level")
	if flag == nil {
		return false
	}
	level, err := logrus.ParseLevel(flag.Value.String())
	return err == nil && level >= logrus.DebugLevel
}

// paint colours text only for a terminal and with NO_COLOR unset. color.NoColor
// is decided from stdout, so it cannot be used for stderr.
func paint(w io.Writer, text string, attr color.Attribute) string {
	f, ok := w.(*os.File)
	if !ok || !isatty.IsTerminal(f.Fd()) || os.Getenv("NO_COLOR") != "" {
		return text
	}
	c := color.New(attr)
	c.EnableColor()
	return c.Sprint(text)
}
