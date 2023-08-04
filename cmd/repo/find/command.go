// Package find provides the 'repo find' command, which helps to discover
// GitHub repositories with certain features, or certain features missing.
package find

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name        = "find"
	description = `Find repositories based on very specific criteria

The --what flag allows to specify what search criteria should be used. When combining several critaria,
a repository will be returned when it's macthing at least one criteria (boolean OR).

Note: archived repositories are always excluded.

Criteria:

- DEFAULT_BRANCH_MASTER     - The default branch is named 'master'.
- HAS_DOCS_DIR              - Has a directory named 'docs' on the root level.
- HAS_PR_TEMPLATE_IN_DOCS   - Has the file docs/pull_request_template.md (which is not the desired location).
- NO_CODEOWNERS             - The /CODEOWNERS file is not present.
- NO_DESCRIPTION            - Repository description is missing.
- README_OLD_CIRCLECI_BAGDE - An outdated CircleCI badge is present in the README.
`
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
		Use:   name,
		Short: description,
		Long:  description,
		RunE:  r.Run,
	}

	f.Init(c)

	return c, nil
}
