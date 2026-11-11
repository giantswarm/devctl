package ci

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/ci/cancel"
	"github.com/giantswarm/devctl/v8/cmd/ci/job"
	"github.com/giantswarm/devctl/v8/cmd/ci/jobs"
	"github.com/giantswarm/devctl/v8/cmd/ci/rerun"
)

const (
	name        = "ci"
	description = "Commands for CircleCI pipelines: the jobs of one, one job's failed output, rerun and cancellation of workflows."
)

type Config struct {
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	jobsCmd, err := jobs.New(jobs.Config{Stderr: config.Stderr, Stdout: config.Stdout})
	if err != nil {
		return nil, err
	}
	jobCmd, err := job.New(job.Config{Stderr: config.Stderr, Stdout: config.Stdout})
	if err != nil {
		return nil, err
	}
	rerunCmd, err := rerun.New(rerun.Config{Stderr: config.Stderr, Stdout: config.Stdout})
	if err != nil {
		return nil, err
	}

	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  description,
	}
	cancelCmd, err := cancel.New(cancel.Config{Stderr: config.Stderr, Stdout: config.Stdout})
	if err != nil {
		return nil, err
	}
	c.AddCommand(cancelCmd)
	c.AddCommand(jobsCmd)
	c.AddCommand(jobCmd)
	c.AddCommand(rerunCmd)

	return c, nil
}
