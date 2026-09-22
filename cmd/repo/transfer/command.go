// Package transfer is `devctl repo transfer`: a declared repository moved to
// another team.
package transfer

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
)

const (
	name      = "transfer"
	shortDesc = "Move a declared repository to another team"
	longDesc  = `Move a declared repository to another team: its entry leaves the giving
team's file and enters the receiving team's file in one pull request, opened
as you, that names both teams. The ask goes to the receiving team's channel
(its member approves), the giving team gets a notice in its standup channel.
The reconciler then re-applies permissions, CODEOWNERS and the catalog
mapping for the new owner.

Examples:
  devctl repo transfer old-tool --to-team team-planeteers --dry-run
  devctl repo transfer old-tool --to-team team-planeteers --reason "Planeteers run it now"`
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
	write.Flags
	ToTeam string
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd, "write nothing")
	cmd.Flags().StringVar(&f.ToTeam, "to-team", "", "The receiving team's slug (team-planeteers). Required.")
}

func (f *flag) Validate() error {
	if err := f.Flags.Validate(); err != nil {
		return microerror.Mask(err)
	}
	if f.ToTeam == "" {
		return microerror.Maskf(client.InvalidFlagError, "--to-team must name the receiving team")
	}
	return nil
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
