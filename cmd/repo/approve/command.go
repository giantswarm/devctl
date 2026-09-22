// Package approve is `devctl repo approve`: the approving review of a
// team-file pull request, as the person, after the team check.
package approve

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

const (
	name      = "approve"
	shortDesc = "Approve a team-file pull request as a member of the deciding team"
	longDesc  = `Approve a team-file pull request in giantswarm/github as you, after
giantswarm-repo-manager has checked on GitHub that you are a member of the
team the change belongs to (the owning team; for a transfer the receiving
team), and land it: merged as you when GitHub lets it, else left to the
auto-merge. A pull request that conflicts with its base (a neighbouring
entry changed first) is re-rendered on the current base before the review.
The author of the pull request and a non-member are refused. This is what
the Approve button of the Slack ask does; approving on GitHub is equivalent.

--dry-run checks the membership and says what the approval would do, writing
nothing.

Examples:
  devctl repo approve 6179
  devctl repo approve 6179 --dry-run --output json`
)

type Config struct {
	Logger *logrus.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	f := &flag{}
	r := &runner{flag: f, logger: config.Logger, stderr: config.Stderr, stdout: config.Stdout, open: client.Open}

	c := &cobra.Command{
		Use:   fmt.Sprintf("%s [flags] PULL-REQUEST", name),
		Short: shortDesc,
		Long:  longDesc,
		Args:  cobra.ExactArgs(1),
		RunE:  r.Run,
	}
	f.Init(c)
	return c, nil
}

type flag struct {
	client.Flags
	DryRun bool
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd)
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Check the membership and say what the approval would do; write nothing.")
}

// pullRequest reads the argument as a pull request number.
func pullRequest(arg string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n <= 0 {
		return 0, microerror.Maskf(client.InvalidFlagError, "expected the pull request's number, got %q", arg)
	}
	return n, nil
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
