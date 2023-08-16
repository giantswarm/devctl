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

Example:

    devctl repo find --what NO_DESCRIPTION --must-have-codeowners

Note: archived repositories are always excluded.

Criteria:

    DEFAULT_BRANCH_MASTER
	
	    The default branch is named 'master'. We want to rename these to 'main'.
	
	HAS_DOCS_DIR
	
	    The repo has a directory named 'docs' on the root level. This is the place
		where we want to store technical documentation for the repo.

	HAS_PR_TEMPLATE_IN_DOCS
	
		Has the file docs/pull_request_template.md, which is not the desired location.
		We want this to be in .github/pull_request_template.md.

	BAD_CODEOWNERS

		Finds repositories with errors in CODEOWNERS files.
	
	NO_CODEOWNERS
	
		There is no CODEOWNERS file in the root folder, which means that the repository
		has no owner. We want repos to have owners, ideally.

	NO_DESCRIPTION
	
		Repository description is missing. We want a meaningful description to be present,
		to understand easily what the repository is about, even from lists.
	
	NO_README

		Repository has no README.md file in the root folder. We want thjis to be present,
		to have some basic info and documentation available.

	README_OLD_CIRCLECI_BAGDE
	
		There is an outdated (broken) CircleCI badge is present in the README. This should
		better get replaced by an up-to-date one, as it can otherwise not fulfil its purpose,
		and broken images never make a good impression.
	
	README_OLD_GODOC_LINK

		The README contains an outdated godoc.org link. Should be pkg.go.dev nowadays.
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
