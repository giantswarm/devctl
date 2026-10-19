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
	description = "Promote the latest release candidate of auto-release repositories, or a named one, to a stable release; one JSON document, exit codes for the outcome."
	long        = `Promote the latest release candidate of auto-release repositories, or with
--version a named one, to a stable release.

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
     With --version vX.Y.Z-rc.N, for exactly one repository, the candidate
     is that one instead of the highest: a published pre-release newer than
     the highest stable release and reachable from the default branch, else
     failed. The workflow must take its version input, else
     outdated_workflow. This promotes a candidate that is built while a
     later one is still building, or the one proven live: the stable tag
     lands on that candidate's commit whatever was cut after it. The
     candidates cut after it on the default branch are named in later[]
     and in the message, as not in the release.
  3. The combined commit status of the candidate's commit: with at least one
     status and a state other than success, not_built. No status at all
     counts as built, as in the workflow. A failed CircleCI status of a job
     the release does not require (sync-china-registry) is judged as
     devctl release wait judges it: a warning in the entry's warnings[],
     not a refusal; any other failed or running context still refuses.
     A not_built entry names in newestBuilt, and in its message, the
     highest other candidate since the stable release that is built:
     promote it with --version, or wait for the refused one.
  4. The workflow is dispatched on the default branch with
     release-type: stable, and with --version also version: the candidate
     (dispatched; would_dispatch with --dry-run).
  5. Once every repository is dispatched, the run each dispatch started is
     followed until it concludes, at most --follow-timeout (20m). The
     workflow resolves the candidate again, checks it, runs the
     repository's stable gate (.github/release-gate.custom.sh) and tags the
     stable release. A run that concludes other than success cuts no
     release: refused, its message naming the run's URL and the errors its
     failed jobs annotated (the gate's "Stable gate refused" among them),
     failedJobs[] each failed job's URL and the end of its log, where the
     gate's own output is. A run that passes leaves the entry dispatched,
     run.conclusion success. A run still going at the timeout, or a
     dispatch GitHub names no run for, stays dispatched with a warning.
     --no-follow returns right after the dispatch.

Repeated repositories are promoted once.

A dispatched repository's release is built from its tag by CircleCI; follow
it with
  devctl release wait <owner/repo> vX.Y.Z
for the stable version of its candidate.

Output: one JSON document on stdout at the end and nothing else (--progress
writes one line per repository to stderr): the envelope (command,
schemaVersion, exitCode, verdict, reason, warnings, startedAt, finishedAt),
team, dryRun, identity{source, login} and repositories[{repository, stable, candidate,
newestBuilt, later, statusState, state, message, run{id, url, status, conclusion},
failedJobs[{name, source, url, logTail, logError}]}], state one of dispatched,
would_dispatch, nothing_to_promote, not_built, not_auto_release,
outdated_workflow, refused, failed.
A team without auto-release repositories is exit 0 with a warning.

Exit codes:
  0  every repository was dispatched and its run passed or still runs
     (would be dispatched, with --dry-run), or has nothing to promote
  1  at least one repository was not promoted: not_built,
     not_auto_release, outdated_workflow, refused or failed; the reason
     names them
  7  usage or tooling: neither or both of repositories and --team, a
     malformed owner/repo, a negative --follow-timeout, --version not of
     the form vX.Y.Z-rc.N or not
     with exactly one repository, or a team without a team file, before
     any repository is read
  8  authentication required: run ` + "`devctl auth login --github-only`" + `

The GitHub token is the App login, or a token in DEVCTL_GITHUB_TOKEN,
GITHUB_TOKEN or OPSCTL_GITHUB_TOKEN that overrides it; identity in the
document names its source and, for the App login, its account. Dispatching
a workflow needs Actions write: the App carries it, and its login is capped
by your own rights on the repository. A 403 on the dispatch, no Actions
write on that repository, is failed with that hint; another refusal from
GitHub is reported per repository. Following reads the run, its jobs, their
logs and annotations: Actions and Checks read.

Examples:
  devctl release promote giantswarm/devctl --dry-run
  devctl release promote giantswarm/devctl giantswarm/klaus
  devctl release promote giantswarm/devctl --version v1.2.3-rc.14
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
