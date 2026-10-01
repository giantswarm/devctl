// Package align is `devctl repo align`: Align now for one repository,
// through the reconciler, in the mode its team-file entry decides.
package align

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

const (
	name      = "align"
	shortDesc = "Align a repository with its declared set-up now, through the reconciler"
	longDesc  = `Align one repository with its declared set-up and the company baseline now.
The repository's entry in repositories/<team>.yaml decides what happens, and
the answer says which mode it was:

  align   the repository has opted in (align: true): the reconciler workflow
          in giantswarm/github is dispatched for it as you and applies the
          planned changes -- merge settings, protection, CircleCI, CODEOWNERS,
          description and visibility, catalog and mapping, a missed release
          build. An alignment changes the repository; read the plan first.
  opt-in  the repository is declared without the field: nothing is dispatched;
          the pull request that sets align: true is opened as you with
          auto-merge armed, the ask goes to the team's channel, and the run of
          the merge aligns the repository.
  check   the repository has no entry (--team names the team): the workflow
          checks it from the team alone and changes nothing.

--dry-run prints the mode, the warning and the planned changes and writes
nothing; without it the workflow is dispatched or the pull request opened.
` + "`devctl repo reconcile`" + ` is the same engine run locally with your own tokens.

Examples:
  devctl repo align my-service --dry-run
  devctl repo align my-service
  devctl repo align orphan --team team-bumblebee --output json`
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
		Use:   fmt.Sprintf("%s [flags] [OWNER/]REPOSITORY", name),
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
	Team   string
	DryRun bool
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd)
	cmd.Flags().StringVar(&f.Team, "team", "", "Team slug (team-bumblebee); required for a repository without an entry, optional otherwise.")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Print the mode, the warning and the planned changes; dispatch nothing, open nothing.")
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
