package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

// UsageError is a command called the wrong way: an argument, a flag or a
// subcommand it does not take. It is printed with the command's usage line
// and a pointer to its help, never with a stack trace.
type UsageError struct {
	Command *cobra.Command
	Err     error
	// Usage prints the command's usage line under the message; an unknown
	// subcommand prints only the pointer to the help, which lists them.
	Usage bool
}

func (e *UsageError) Error() string { return e.Err.Error() }

func (e *UsageError) Unwrap() error { return e.Err }

// usageKinds are the microerror kinds the runners' flag and argument
// validation returns: mistakes in the call, printed like a [UsageError].
var usageKinds = []string{"invalidArgError", "invalidFlagError", "invalidFlagsError"}

// isUsageKind reports whether err is a runner's flag or argument validation
// error.
func isUsageKind(err error) bool {
	var e *microerror.Error
	return errors.As(err, &e) && slices.Contains(usageKinds, e.Kind)
}

// guardUsage makes every command in the tree under root report a wrong call
// as a [UsageError]: a flag that does not parse, arguments its Args rejects,
// and, for a command that only groups others, a word that is none of its
// subcommands (cobra would print the group's help and exit 0).
func guardUsage(root *cobra.Command) {
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return &UsageError{Command: c, Err: err, Usage: true}
	})
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		switch {
		case c.Args != nil:
			c.Args = explainArgs(c.Args)
		case c.HasSubCommands():
			c.Args = subcommandArgs
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// subcommandArgs rejects any argument of a command that only groups
// subcommands, suggesting the subcommands it resembles.
func subcommandArgs(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	message := fmt.Sprintf("unknown command %q for %q", args[0], c.CommandPath())
	if c.SuggestionsMinimumDistance <= 0 {
		c.SuggestionsMinimumDistance = 2
	}
	if suggestions := c.SuggestionsFor(args[0]); len(suggestions) > 0 {
		message += fmt.Sprintf("; did you mean %s?", quoteAll(suggestions, " or "))
	}
	return &UsageError{Command: c, Err: errors.New(message)}
}

// explainArgs words the error of validate in terms of the command's usage
// line: the arguments that are one too many, or the parameters that are
// missing. An error it cannot place, an invalid value for one, is kept.
func explainArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		err := validate(c, args)
		if err == nil {
			return nil
		}
		return &UsageError{Command: c, Err: argsError(c, validate, args, err), Usage: true}
	}
}

func argsError(c *cobra.Command, validate cobra.PositionalArgs, args []string, err error) error {
	for n := len(args) - 1; n >= 0; n-- {
		if validate(c, args[:n]) == nil {
			return fmt.Errorf("unexpected %s %s", plural("argument", len(args)-n), quoteAll(args[n:], " "))
		}
	}

	filler := "x"
	if len(c.ValidArgs) > 0 {
		filler = c.ValidArgs[0]
	}
	params := parameters(c)
	for n := 1; len(args)+n <= len(params); n++ {
		padded := append(slices.Clone(args), slices.Repeat([]string{filler}, n)...)
		if validate(c, padded) == nil {
			return fmt.Errorf("missing %s", strings.Join(params[len(args):len(args)+n], " "))
		}
	}

	return err
}

// parameters are the positional parameters of the command's usage line:
// "status [flags] [OWNER/]REPOSITORY" has one, [OWNER/]REPOSITORY; a
// bracketed "[FILE ...]" is one.
func parameters(c *cobra.Command) []string {
	var params []string
	var open string
	for _, field := range strings.Fields(c.Use)[1:] {
		if open != "" {
			field = open + " " + field
		}
		if strings.Count(field, "[") > strings.Count(field, "]") || strings.Count(field, "<") > strings.Count(field, ">") {
			open = field
			continue
		}
		open = ""
		if field == "[flags]" || strings.HasPrefix(field, "-") || strings.HasPrefix(field, "[-") {
			continue
		}
		params = append(params, field)
	}
	return params
}

func quoteAll(words []string, sep string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = fmt.Sprintf("%q", w)
	}
	return strings.Join(quoted, sep)
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
