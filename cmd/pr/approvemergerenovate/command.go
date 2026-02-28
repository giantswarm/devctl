package approvemergerenovate

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name        = "approvemergerenovate"
	longCmd     = "approve-merge-renovate"
	description = "Approves and auto-merges Renovate PRs matching a search query. If no query is provided, presents an interactive group selector."
	usage       = "approve-merge-renovate [query]"
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

	r := &runner{
		flag:   f,
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:     usage,
		Short:   description,
		Long:    description,
		Aliases: []string{name, "amr"},
		Example: `  # Interactive mode - select from grouped PRs
  devctl pr amr

  # Interactive mode - group by repository instead of dependency
  devctl pr amr --by-repo

  # Direct mode - search for specific PRs
  devctl pr amr "architect v1.2.3"
  
  # Watch mode with query
  devctl pr amr --watch "architect v1.2.3"`,
		RunE: r.Run,
	}

	f.Init(c)

	return c, nil
}
