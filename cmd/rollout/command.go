package rollout

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/rollout/wait"
)

const (
	name        = "rollout"
	description = "Commands for releases on installations: wait until one runs there."
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

	waitCmd, err := wait.New(wait.Config{Stderr: config.Stderr, Stdout: config.Stdout})
	if err != nil {
		return nil, err
	}

	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  description,
	}
	c.AddCommand(waitCmd)

	return c, nil
}
