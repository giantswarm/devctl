package merge

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	name        = "merge <owner/repo> <number>"
	description = "Wait until the pull request's head is green, squash-merge it, delete the branch and wait until the release it triggers is pullable; one JSON document, an exit code."
	long        = `Wait until the outcome of a pull request's CI is known (the wait of devctl pr
wait), merge it and delete its branch, then wait until the release the merge
triggers is pullable (the wait of devctl release wait --pr), print one JSON
document on stdout and exit with a code that says what happened.

Refused before any wait: a draft, a closed or merged pull request, one
conflicting with its base or behind a base that requires branches to be up to
date (exit 3, unless --update-branch); a pull request opened by another human
(exit 5: bots, GitHub Apps, Giant Swarm's automation accounts -- taylorbot,
which opens every generated release pull request, and architectbot -- and the
caller's own pull requests are fine); a repository whose team-file entry in
giantswarm/github says agentMerge: false (exit 5, naming the field). A
repository no team file declares is not opted out.

Green lands through the merge API as a squash (--rebase: a rebase merge) with
the judged head as the expected head, so a head that moved is not merged; the
squash commit's subject is the pull request's title with its number. The head
branch is deleted through the refs API; a head in a fork is left alone. A base
with a merge queue is enqueued instead, the pull request waited for until the
queue merged it, then the branch deleted. Nothing reads a protection setting
to change it and nothing writes one. The merge is made as you, with the user
token of devctl auth login: the review rule is passed through the bypass the
repository's ruleset grants your team or the repository's admins (the
alignment engine writes the owning team and the repository admins, beside
the devctl App, as bypass actors for pull requests; an App's bypass covers
its installation tokens, which devctl does not hold). In a repository
another team owns, unless you are one of its admins, your own green pull
request is declined until a reviewer with write access approves it: exit 3,
the reason naming the ruleset, its bypass actors and the owning team.

--update-branch: a head behind a strict base is updated from the base
(GitHub's Update branch) and the new head is what the wait judges and the
merge lands. A merge GitHub declines as the pull request stands (a rule blocks
it, the base or the head moved) is exit 3 with GitHub's sentence.

The release: in a repository whose release model is auto-release, the tag
auto-release puts on the merge commit is waited for and then every image and
chart of it until each resolves to a digest in its registry, with the rules of
devctl release wait, within --release-timeout (the CI wait and a merge queue
have --timeout). No release follows a merge in a repository that does not tag
merge commits (the legacy release workflow, no release workflow at all) or
whose auto-release run finished without a tag because the commits warrant no
bump: that is exit 0, and the document says so. --no-release-wait ends the
command at the merge; devctl release wait remains the command for
a release on its own (a version, --catalog).

Tokens come from the keychain (` + "`devctl auth login`" + `); the CircleCI token is
required only when CircleCI is consulted, as in devctl pr wait and devctl
release wait.

The document (schemaVersion 1) is devctl pr wait's (command, exitCode,
verdict, reason, warnings, startedAt, finishedAt, repository, number, headSha,
baseRef, checks[], circleci{}, actions[], unfinished[]) plus mergeCommitSha
(the merge commit, empty when nothing merged), method (squash|rebase),
branchDeleted, enqueued and release: null with --no-release-wait or when
nothing merged, otherwise the release wait's verdict (available, no_release,
ci_failed, timeout, ...) and reason with its result (tag, sha, releaseModel,
ciModel, artifacts[{kind, reference, digest, state}], pipeline, actions). See
docs/pr-merge.md.

Exit codes 6 and 9 mean the pull request was merged; never merge it again.

Exit codes:
  0  merged (or enqueued and merged by the queue), branch deleted, and the
     release is pullable or no release follows the merge
  1  red: a check, status, run or workflow failed, or the queue dropped it
  2  timeout before an outcome; unfinished names what was still open
  3  not applicable: draft, closed, merged, conflicting, behind a strict base
     (without --update-branch), or GitHub declined the merge as it stands
  4  a required status context never reported within the timeout
  5  refused: another human's pull request, or agentMerge: false
  6  merged, and the release failed: the merge commit's auto-release run or
     the tag's pipeline failed (release.pipeline.failedJobs)
  7  usage or a tooling failure
  8  authentication required; reason names the devctl auth login to run
  9  merged, and the release was not confirmed: --release-timeout passed
     first, or the release wait could not judge it (release.verdict and the
     reason say which; devctl release wait <owner/repo> --pr <n> resumes it)`
	example = `  devctl pr merge giantswarm/devctl 2278
  devctl pr merge giantswarm/devctl 2278 --timeout 45m --release-timeout 30m --progress
  devctl pr merge giantswarm/devctl 2278 --no-release-wait
  devctl pr merge giantswarm/kagent-upstream 12 --rebase
  devctl pr merge giantswarm/devctl 2278 --update-branch`
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
		flag:            f,
		stderr:          config.Stderr,
		stdout:          config.Stdout,
		requireGitHub:   authstore.RequireGitHub,
		requireCircleCI: authstore.RequireCircleCI,
		endpoints:       agentcli.EndpointsFromEnv,
		clock:           agentcli.SystemClock,
	}

	c := &cobra.Command{
		Use:     name,
		Short:   description,
		Long:    long,
		Example: example,
		// The arguments are checked by the runner: a usage error is exit 7
		// with a document, like every other outcome.
		Args: cobra.ArbitraryArgs,
		RunE: r.Run,
	}
	c.SetFlagErrorFunc(r.FlagError)

	f.Init(c)

	return c, nil
}
