// Package setlifecycle is `devctl repo set-lifecycle`: a declared repository
// deprecated, archived or deleted through its team-file entry.
package setlifecycle

import (
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
)

const (
	name      = "set-lifecycle"
	shortDesc = "Deprecate, archive or delete a declared repository"
	longDesc  = `Deprecate, archive or delete a declared repository by setting lifecycle in
its team-file entry, in a pull request opened as you; the ask goes to the
owning team's channel and a member's Approve (or an approving review on
GitHub) lands it.

  deprecated  security-only Renovate and a catalog flag
  archived    the reconciler unfollows the repository on CircleCI and
              archives it on GitHub; the entry stays as the record
  deleted     the reconciler unfollows the repository on CircleCI and deletes
              it on GitHub -- code, issues, pull requests, releases and
              packages with it (an organization owner can restore it for 90
              days); the entry stays as the record of the deletion. Needs
              --confirm with the repository's name.

An entry without align: true gets it beside the lifecycle: the change opts
the repository in to alignment, else the reconciler would record the
lifecycle and apply nothing.

Examples:
  devctl repo set-lifecycle old-tool deprecated --dry-run
  devctl repo set-lifecycle old-tool archived --reason "replaced by new-tool"
  devctl repo set-lifecycle scratch-repo deleted --confirm scratch-repo --reason "a throwaway"`
)

var lifecycles = []string{"deprecated", "archived", "deleted"}

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
		Use:   fmt.Sprintf("%s [flags] [OWNER/]REPOSITORY deprecated|archived|deleted", name),
		Short: shortDesc,
		Long:  longDesc,
		Args:  cobra.ExactArgs(2),
		RunE:  r.Run,
	}
	f.Init(c)
	return c, nil
}

type flag struct {
	write.Flags
	Confirm string
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd, "write nothing")
	cmd.Flags().StringVar(&f.Confirm, "confirm", "", "For deleted: the repository's name, typed again.")
}

// validateLifecycle checks the second argument and the confirmation a
// deletion needs.
func (f *flag) validateLifecycle(repository, lifecycle string) error {
	if !slices.Contains(lifecycles, lifecycle) {
		return microerror.Maskf(client.InvalidFlagError, "the lifecycle must be deprecated, archived or deleted, got %q", lifecycle)
	}
	if lifecycle == "deleted" && f.Confirm == "" {
		return microerror.Maskf(client.InvalidFlagError, "deleting %s needs --confirm %s: the repository's name typed again", repository, repository)
	}
	return nil
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
