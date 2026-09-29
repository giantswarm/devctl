package promote

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

const (
	name        = "promote [<owner/repo>...]"
	description = "Promote the latest release candidate of auto-release repositories to a stable release; one JSON document, exit codes for the outcome."
	long        = `Promote the latest release candidate of auto-release repositories to a stable
release.

The auto-release workflow (.github/workflows/zz_generated.auto_release.yaml)
cuts a release candidate vX.Y.Z-rc.N, a GitHub pre-release, on every
releasable push. A stable release is cut only by running that workflow by
hand with release-type stable. This command does that for the repositories
named as owner/repo, or with --team for every entry of the team's file in
giantswarm/github whose release model is auto-release (releaseWorkflow,
defaulting from gen.ci.generate), in the giantswarm organization. Pass
exactly one of the two.

For each repository, on its default branch:
  1. The workflow file must be there (else not_auto_release) and carry the
     step "Resolve the candidate to promote" of devctl v8.102.0 or later
     (else outdated_workflow: an older workflow's release-type stable tags
     the branch head instead of promoting a candidate).
  2. The candidate is the highest vX.Y.Z-rc.N GitHub release newer than
     the highest stable vX.Y.Z release, counting only tags reachable from
     the default branch, as the workflow does (semver order: rc.10 after
     rc.9). A candidate or stable release of another branch is skipped.
     None: nothing_to_promote. A candidate that is a full release, not a
     pre-release, is failed, as the workflow refuses it; no lower candidate
     is taken. The command reads releases, not tags: a candidate tag whose
     GitHub release was deleted, or is a draft, is not seen, while the
     workflow stops on it.
  3. The combined commit status of the candidate's commit: with at least one
     status and a state other than success, not_built. No status at all
     counts as built, as in the workflow.
  4. The workflow is dispatched on the default branch with
     release-type: stable (dispatched; would_dispatch with --dry-run).

Repeated repositories are promoted once.

The workflow resolves the candidate again, checks it and promotes it; this
command does not wait for the run. Follow each one with
  devctl release wait <owner/repo> vX.Y.Z
for the stable version of its candidate.

Output: one JSON document on stdout at the end and nothing else (--progress
writes one line per repository to stderr): the envelope (command,
schemaVersion, exitCode, verdict, reason, warnings, startedAt, finishedAt),
team, dryRun and repositories[{repository, stable, candidate, statusState,
state, message}], state one of dispatched, would_dispatch,
nothing_to_promote, not_built, not_auto_release, outdated_workflow, failed.
A team without auto-release repositories is exit 0 with a warning.

Exit codes:
  0  every repository was dispatched (would be, with --dry-run) or has
     nothing to promote
  1  at least one repository was not dispatched: not_built,
     not_auto_release, outdated_workflow or failed; the reason names them
  7  usage or tooling: neither or both of repositories and --team, a
     malformed owner/repo, a team without a team file, or a token that
     cannot dispatch (the App login without --dry-run), before any
     repository is read
  8  authentication required: run ` + "`devctl auth login --github-only`" + `

The GitHub token is the App login, or a token in DEVCTL_GITHUB_TOKEN,
GITHUB_TOKEN or OPSCTL_GITHUB_TOKEN that overrides it. Dispatching a
workflow needs Actions write, which the App login does not carry: without
--dry-run, set one of the variables to a token that has it. A 403 on the
dispatch, a token without Actions write on that repository, is failed with
that hint; another refusal from GitHub is reported per repository.

Examples:
  devctl release promote giantswarm/devctl --dry-run
  devctl release promote giantswarm/devctl giantswarm/klaus
  devctl release promote --team team-bumblebee --progress`
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

	f := &flag{}

	r := &runner{
		gate:   versiongate.Check,
		flag:   f,
		stderr: config.Stderr,
		stdout: config.Stdout,
		open:   releasepromote.OpenSources,
	}

	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  long,
		// The arguments are checked by the runner: a usage error is exit 7
		// with a document, like every other outcome.
		Args:        cobra.ArbitraryArgs,
		RunE:        r.Run,
		Annotations: agentcli.AgentFacing(),
	}
	c.SetFlagErrorFunc(r.FlagError)

	f.Init(c)

	return c, nil
}
