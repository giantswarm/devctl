package llm

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "llm"
	shortDescription = `Generates instructions for coding agents in AGENTS.md.`
	longDescription  = `Generates instructions for coding agents in AGENTS.md.

devctl gen llm --flavour app --language go

Writes Giant Swarm's shared instructions into a devctl-managed block at the
top of AGENTS.md. The rest of the file holds the repository's own
instructions, which take precedence. Claude files get an import of AGENTS.md,
.claude/skills links to .agents/skills, and the .cursor/rules files that
earlier versions generated are deleted.

A repository with instructions that are not in a devctl-managed block yet is
left unchanged, with a warning. Migrate it with --migrate-instructions, which
moves the content of CLAUDE.md or .claude/CLAUDE.md to AGENTS.md and the
skills in .claude/skills to .agents/skills. Check the result before
committing it.

Use --dry-run to see the changes or the reasons for skipping first. See
docs/gen.md for details.
`
)

type Config struct {
	Logger micrologger.Logger
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
		Args:  cobra.NoArgs,
		Short: shortDescription,
		Long:  longDescription,
		RunE:  r.Run,
	}

	f.Init(c)

	return c, nil
}
