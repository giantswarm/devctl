package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

// TestHelp ensure we can call '--help' on the command returned by New
// and all its children.
func TestHelp(t *testing.T) {
	var err error

	var logger micrologger.Logger
	{
		c := micrologger.Config{}

		logger, err = micrologger.New(c)
		if err != nil {
			t.Fatal(err)
		}
	}

	c := Config{
		Logger: logger,
	}

	rootCmd, err := New(c)
	if err != nil {
		t.Fatal(err)
	}

	commands := getAllCommands(rootCmd)

	for _, cmd := range commands {
		fullname := buildName(cmd)
		t.Run(strings.Join(fullname, " "), func(*testing.T) {
			buf := new(bytes.Buffer)

			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			// Using rootCmd directly otherwise, we have to set arguments via os.Args which feels wrong.
			rootCmd.SetArgs(append(fullname, "--help"))
			err = rootCmd.Execute()
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// getAllCommands find the list of all commands containing cmd and its children.
func getAllCommands(cmd *cobra.Command) (cmds []*cobra.Command) {
	cmds = append(cmds, cmd)

	for _, c := range cmd.Commands() {
		cmds = append(cmds, getAllCommands(c)...)
	}

	return cmds
}

// buildName return the full command arguments to use.
// e.g. to run 'devctl foo bar' we need 'foo bar' arguments.
func buildName(cmd *cobra.Command) (fullname []string) {
	// This condition is required otherwise 'devctl' is always added as first argument.
	if cmd.HasParent() {
		fullname = append(buildName(cmd.Parent()), cmd.Name())
	}

	return fullname
}
