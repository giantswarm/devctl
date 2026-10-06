# Merging a pull request: `devctl pr merge`

```nohighlight
devctl pr merge <owner/repo> <number> [--timeout 30m] [--release-timeout 30m] [--no-release-wait]
                [--rebase] [--update-branch] [--dispatch <owner>/<repo>/<workflow file>[@<ref>]]
                [--progress] [--failed-log [--failed-log-lines 50]] [--detach [--on-done <command>]]
/home/teemow/.go/bin/beekeeper gate -- devctl pr merge status [<handle>]
```

One blocking call that waits until the pull request's head is green (the wait of
[`devctl pr wait`](pr-wait.md), verdicts and codes included), merges it through the merge API,
deletes its branch through the refs API, and then waits until the release the merge triggers is
pullable (the wait of [`devctl release wait --pr`](release-wait.md)). It prints one JSON document on
stdout at the end and nothing else; the exit code says what happened. `--progress` writes one line
per step to stderr.

An agent that merges its own pull request makes this one call and is done when it exits 0: CI was
green, the pull request is merged, and the release is pullable or none follows the merge. Nothing
before a consumer bump or a rollout needs a second wait. A caller that cannot be held that long starts
the same merge with `--detach` and reads its outcome later ([below](#detached-merge---detach)).

The tokens come from the OS keychain (`devctl auth login`, see [auth.md](auth.md)): the GitHub token
before the first request, the CircleCI token only once the head or the tag turns out to carry a
CircleCI configuration for a repository CircleCI builds, as in `pr wait` and `release wait`. The GitHub identity follows the repository's owner: the
App login for giantswarm, where the devctl App is installed, and your own `gh` login (`gh auth token` of the
real `gh`, never a `gh` link to devctl, and no environment variable) for every other owner, with the same
semantics; `identity` in the document says which, `app` or `gh`. Without a `gh` login such a pull request is
exit 8 naming `gh auth login`; a repository neither identity can read is exit 7 naming the missing
installation and the read access your `gh` login lacks.
The merge outside giantswarm is made as your `gh` login, through whatever that repository's protection grants
you.

## What is refused before the wait

Every refusal comes before the first poll and before anything is written.

| Code | Verdict | Refused |
|---|---|---|
| 3 | `not_applicable` | A **draft**; a **closed** or **merged** pull request; one that **conflicts** with its base; one **behind** a base whose protection requires branches to be up to date, unless `--update-branch` is given. The rule is `pr wait`'s. |
| 5 | `refused` | A pull request **another human opened**. The author is compared with the account the token acts as (the login of the keychain record, or `GET /user`); a bot, a GitHub App (user type `Bot`) or a `[bot]` login is fine, so a Renovate or Dependabot pull request merges. Giant Swarm's automation accounts that GitHub carries as plain users are fine too -- `taylorbot`, which opens the release pull request of every repository on devctl-generated CI, and `architectbot` -- each matched on its numeric account id as well as its login, so the login alone opens nothing. The reason names every author the merge accepts, and for an automation login whose account id is not the pinned one, both ids. |
| 5 | `refused` | A repository whose team-file entry **opts out of agent merges**: the entry named after the repository in `repositories/<team>.yaml` of `giantswarm/github` at `main` says `agentMerge: false`. The entry is read the way `devctl repo` reads one; the reason names the field. An entry without the field, a repository no team file declares and a repository outside the organisation the team files declare are not opted out. Team files the token cannot read are a tooling failure (7), not a guess. |

| 5 | `refused` | **Unanswered review**: a reviewer's feedback newer than the head that nobody answered (see [Unanswered reviews](#unanswered-reviews)). Read here and again right before the merge call. |

A mergeable state GitHub has not computed yet (`unknown`) is read again a few times before the
refusals are applied, so a pull request just pushed is judged on its state and not on GitHub's
laziness.

## Unanswered reviews

A review in state `COMMENTED` does not block GitHub's protection, so a pull request would merge seconds after
a reviewer asked for changes. `devctl pr merge` reads the pull request's reviews, review comments (on the diff,
replies included) and conversation comments after the refusals above, and again **immediately before the merge
call** (or the enqueue), so feedback that lands while the checks run is caught too. Each read is fresh. It
refuses with exit 5, `refused`, while one item is unanswered:

- **Whose**: a person other than the pull request's author and the caller. Bots, GitHub Apps and the
  automation accounts (as in the author rule) never hold a merge.
- **Which**: a review in state `COMMENTED` or `CHANGES_REQUESTED` (not `APPROVED`, `DISMISSED` or pending), a
  review comment, a conversation comment.
- **Newer than the head**: a review or review comment on the head commit, or any item written after the head
  commit's committer date. Feedback on an older commit and before it was answered by the later commit.
- **Answered** by anything the author or the caller wrote on the pull request after it (a reply in the thread,
  a comment on the conversation, a review), by an `APPROVED` review of the same reviewer no older than it, or by
  a new commit. Another reviewer's approval answers nothing.

The reason names the count and the first five items (kind, state, reviewer, link) and what clears them;
`unansweredReviews[]` in the document lists every one. The caller answers the feedback (or pushes the fix) and
runs the merge again.

## The wait

The wait is `pr wait`'s, with its verdicts and exit codes: 1 red, 2 timeout, 3 not applicable, 4 a
required context never reported. Nothing is merged on any of them. `--failed-log` is `pr wait`'s too: a red
wait prints the tail of each failed job's log to stderr and carries it in `failedJobs[]`. A head that changes under the
wait resets it to the new head with a warning. Its reads, and the release wait's, are retried the way
`pr wait`'s are ([Polling](pr-wait.md#polling)): a reset connection or a 5xx is a warning and another
try, exit 7 only after eight in a row; a spent rate limit is a warning and a wait for its reset, exit 2
before the merge (9 after it) when the reset falls after the deadline. The merge, the branch update and the branch deletion are writes
and are sent once.

## The merge

Green lands with one call of the merge API, `PUT /repos/{owner}/{repo}/pulls/{number}/merge`:

- **Squash** by default; `--rebase` is a rebase merge, for repositories whose convention is one
  commit per patch (the upstream-line forks). Never a merge commit.
- The judged head SHA goes along as the expected head: a head that moved between the verdict and
  the merge is not merged (GitHub answers 409), and the pull request is re-read after the wait for
  the same reason (a retitle during the wait names the squash commit).
- The squash commit's subject is the pull request's title with its number, `<title> (#<number>)`:
  the title the title check accepted is what the auto-release reads.
- Before the merge call, the body as it stands after the wait is read for GitHub's closing keywords (`close`,
  `fix`, `resolve` in any tense and case, an optional colon) directly before a reference. One that names a pull
  request (a `/pull/N` URL, or a `#N`, `GH-N` or `owner/repo#N` GitHub reports as a pull request) or an item of
  another repository is a warning, on the progress stream and in `warnings`: GitHub closes it when the pull request
  merges, without a prompt, a pull request unmerged. The warning names the phrase and the item; nothing is refused and
  the exit code does not change. A closing keyword before an issue of the same repository is the intended use and
  stays silent, as does a number that does not exist.
- The head branch is deleted through the refs API, `DELETE /repos/{owner}/{repo}/git/refs/heads/{branch}`;
  a branch GitHub deleted already is fine. A head that lives in a fork is left alone, with a warning.
- **No protection setting is read to be changed and none is written**: no branch protection, no
  ruleset, no `enforce_admins`. The merge is made as the caller, with the user token of `devctl auth
  login`; devctl holds no installation token. GitHub evaluates that token as the person, so the
  review rule is passed through the bypass the repository's ruleset grants the person: the alignment
  engine writes the repository's owning team and the repository admins, beside the devctl App, as bypass
  actors in `pull_request` mode (`devctl repo reconcile`). The App's bypass covers the App's installation
  tokens, none of which devctl uses. A member of the owning team, or an admin of the repository, merges
  their own green pull request past the required review; anyone else's is declined (405) until a reviewer
  with write access approves it, and nothing is changed to get past it.

A merge GitHub declines as the pull request stands (405: a rule blocks it, the base moved under a
strict protection; 409: the head moved) or for the token's permissions (403: the devctl App lacks a
permission the merge needs, such as `workflows` for a pull request that changes a workflow) is exit 3,
`not_applicable`, with GitHub's sentence and the login devctl acted as (`devctl acts as <login>`) as
the reason. Declined for the review rule, the reason goes on to say which rulesets
of the base carry a pull request rule, and of those the ones GitHub says the caller cannot bypass
(`current_user_can_bypass`) as the blockers with their bypass actors, the ones the caller bypasses
apart (read, never written; an App actor is marked as covering installation tokens only), and the
team whose file declares the entry: one of its members or a repository admin merges it, or a reviewer
with write access approves it first. Past a blocking ruleset without bypass actors no member or admin
merges: only an approving review or a change to that ruleset does, and a ruleset devctl did not create
is named a `foreign-ruleset` the reconciler leaves to the owning team. A base whose review requirement
no ruleset carries is on classic branch protection: the repository is aligned first, never merged
past it. The reason names the entry to opt in (`align: true` on `the entry <repo> in
repositories/<team>.yaml of giantswarm/github`; a repository no team file declares is declared first
with `devctl repo adopt`): the reconciler then writes the devctl ruleset, whose bypass lets the
owning team and the repository admins merge their own green pull requests. devctl never lifts
`enforce_admins`, and the devctl App's token carries no Administration permission to do so.

### A base with a merge queue

When the rules in effect on the base (`GET /repos/{owner}/{repo}/rules/branches/{base}`) include a
`merge_queue` rule, the merge API is not the way: the pull request is **enqueued** (the GraphQL
mutation `enqueuePullRequest`; REST has none), the command waits until the queue merged it, within
the same `--timeout`, then deletes the branch. `enqueued` is true in the document and
`mergeCommitSha` is the commit the queue produced. A pull request the queue drops (closed without a
merge) is exit 1; the deadline is exit 2 with `unfinished` naming the queue.

### `--update-branch`

A head behind a base that requires branches to be up to date cannot merge as it is, and a head
behind any other base may be red only because its old base was. With `--update-branch` the command
asks GitHub to merge the base into the head before any check is judged (the **Update branch**
button, `PUT .../pulls/{number}/update-branch` with the current head as the expected head), reads
the pull request until the new head is on it, and the wait judges that head: the CI of the updated
branch is what turns green, and the merge lands it. Behind is GitHub's mergeable state for a strict
base, and for every other base the comparison of base and head (`GET .../compare/{base}...{head}`,
`behind_by`); a head that is not behind is judged as it is. Without the flag, behind a strict base
is exit 3 and a head behind any other base is judged as it is.

## The release

After the merge the command goes on into the wait of `devctl release wait <owner/repo> --pr
<number>`, given the merge commit it just produced, with the models, artifact names, registry probes
and tag-pipeline rules described in [release-wait.md](release-wait.md). `--release-timeout` (30
minutes by default) bounds it; `--timeout` bounds the CI wait and a merge queue, so a slow CI does not
eat into the release's time.

- **A release follows** in a repository on the auto-release model when auto-release tags the merge
  commit: the tag, then the tag pipeline, then every image and chart resolving to a digest. Exit 0
  once all of them are pullable and the tag pipeline is green.
- **No release follows**, exit 0 all the same, when the repository does not tag merge commits (the
  legacy release workflow, or no release workflow and no team-file entry that declares a release
  model), or when the merge commit's auto-release run finished without a tag because the commits
  since the last release warrant no bump (a `docs:` or `chore:` subject). The run finishing is the
  answer; the command does not wait out the timeout for a tag that will never come. `release.verdict`
  is `no_release` and `release.reason` says why.
- **The release failed**, exit 6: the merge commit's auto-release run failed before it tagged, or a
  workflow of the tag pipeline failed; `release.pipeline.failedJobs` names the jobs. The pull request
  is merged; the fix is the next pull request or a rerun of the failed workflow.
- **The release is not confirmed**, exit 9: `--release-timeout` passed first, the release wait could
  not judge it (a missing CircleCI token, sources that disagree about the artifacts, a registry answer
  that is neither a digest nor "manifest unknown"), or the merge commit's auto-release run was
  cancelled because a newer push to the branch superseded it (that push's tag carries the merge).
  `release.verdict` and the reason say which; after a timeout or a login, `devctl release wait
  <owner/repo> --pr <number>` resumes the wait.

Exit codes 6 and 9 say that the pull request was merged: the caller never merges it again.
`--no-release-wait` ends the command at the merge and leaves `release` null. `devctl release wait`
remains the command for a release on its own: by version, or with `--catalog`.

## After the merge: a workflow dispatch

A site that is generated from merges and releases on a schedule (a team's product magazine) shows a
merge only at its next refresh. `--dispatch <owner>/<repo>/<workflow file>[@<ref>]` tells it at once:
once the pull request is merged, after the release wait whatever it ended with, the workflow of that
file in `.github/workflows` of the repository is dispatched through its `workflow_dispatch` trigger
(`POST /repos/{owner}/{repo}/actions/workflows/{file}/dispatches`), on the ref given or the
repository's default branch, with three inputs:

| Input | Value |
|---|---|
| `repository` | The merged pull request's repository, `<owner/repo>`. |
| `pull_request` | Its number. |
| `release` | The tag the merge released (`release.tag`); empty when no release follows the merge, when the release was not confirmed in time, or with `--no-release-wait`. |

The workflow declares the three inputs (GitHub refuses a dispatch carrying an input the workflow does
not declare, with `Unexpected inputs provided`); a workflow of the magazine's kind reads them and
refreshes. The dispatch is made with the same token as the merge: the devctl App for a repository
under giantswarm (Actions write, see [auth.md](auth.md)), your `gh` login elsewhere.

`DEVCTL_MERGE_DISPATCH` carries the same value for every merge on a machine (an agent desk whose
merges all feed one magazine); the flag wins over it. A value in another form is exit 7 before any
request. Without the flag and the variable nothing changes.

The dispatch never changes the merge's outcome: one GitHub refuses (an unknown workflow, an
undeclared input, a token without Actions write, a repository it cannot read) is a warning in
`warnings` and the reason in `dispatch.reason`, and the exit code is the merge's. Nothing is
dispatched when nothing merged.

```nohighlight
devctl pr merge giantswarm/devctl 2278 --dispatch giantswarm/team-magazine/refresh.yaml
DEVCTL_MERGE_DISPATCH=giantswarm/team-magazine/refresh.yaml devctl pr merge giantswarm/devctl 2278
```

## Detached merge: `--detach`

The blocking call holds its caller through the CI wait, the merge and the release wait, often for
more than ten minutes. `--detach` hands that wait off: the call is checked exactly as the blocking one
is (arguments, flags, the version check and the token, so a wrong call is still exit 7 or 8 at once),
then the same merge starts in a process of its own, in a session of its own so it outlives the
caller's terminal, and the call returns within seconds with exit 0, verdict `detached`:

```json
{
  "command": "pr merge",
  "exitCode": 0,
  "verdict": "detached",
  "reason": "giantswarm/devctl#2278 merges in pid 41711: devctl pr merge status giantswarm-devctl-2278-20261007T091500Z reads the outcome",
  "repository": "giantswarm/devctl",
  "number": 2278,
  "identity": "app",
  "handle": "giantswarm-devctl-2278-20261007T091500Z",
  "pid": 41711,
  "log": "/home/me/.local/state/devctl/merges/giantswarm-devctl-2278-20261007T091500Z/output.log",
  "document": "/home/me/.local/state/devctl/merges/giantswarm-devctl-2278-20261007T091500Z/document.json",
  "status": "devctl pr merge status giantswarm-devctl-2278-20261007T091500Z"
}
```

The detached merge is the blocking command with every flag given (`--progress` always, so its log
shows each step) and the environment of the call (`DEVCTL_MERGE_DISPATCH` included). It lives in
`$XDG_STATE_HOME/devctl/merges/<handle>/` (`~/.local/state/devctl/merges/` without the variable):
`output.log`, then `document.json`, the merge's document exactly as the blocking call prints it,
written whole when the merge ended. A second `--detach` of a pull request whose detached merge still
runs is exit 3 naming its handle. Ended merges are removed 30 days after they started.

`devctl pr merge status <handle>` reads it: one JSON document with `handle`, `state`, `repository`,
`number`, `pid`, `log`, `merge` (the merge's document once it ended, otherwise `null`) and `onDone`.
Its exit code is the merge's own once the merge ended, so a caller reads it as it would the blocking
call's (0 merged and released, 6 and 9 merged, 1 to 5 nothing merged); while the merge runs it is
10, verdict `running`. A merge whose process is gone without a document (killed, the machine
restarted) is `state: lost`, exit 7: whether it merged is GitHub's to say, and
`devctl release wait <owner/repo> --pr <n>` confirms its release. Without a handle, `jobs[]` lists
every detached merge of the machine, the newest first, with `state` and `exitCode`.

`--on-done <command>` (with `--detach` only) is the callback: once the detached merge wrote its
document, the command runs in `sh -c` (`cmd /C` on Windows), for at most ten minutes, with

| Variable | Value |
|---|---|
| `DEVCTL_MERGE_HANDLE` | The handle. |
| `DEVCTL_MERGE_EXIT_CODE` | The merge's exit code. |
| `DEVCTL_MERGE_DOCUMENT` | The path of the merge's document. |
| `DEVCTL_MERGE_REPOSITORY`, `DEVCTL_MERGE_NUMBER` | The pull request. |

Its output goes to the log, its exit code into `onDone` of the status document; it never changes the
merge's outcome. A remote callback is `--dispatch`, which works the same detached.

```nohighlight
/home/teemow/.go/bin/beekeeper gate -- devctl pr merge giantswarm/devctl 2278 --detach
/home/teemow/.go/bin/beekeeper gate -- devctl pr merge giantswarm/devctl 2278 --detach --on-done 'notify-send "merge $DEVCTL_MERGE_NUMBER: exit $DEVCTL_MERGE_EXIT_CODE"'
/home/teemow/.go/bin/beekeeper gate -- devctl pr merge status giantswarm-devctl-2278-20261007T091500Z
/home/teemow/.go/bin/beekeeper gate -- devctl pr merge status
```

The blocking call stays the default. A wrapper that already runs the merge outside its caller and
reads the blocking call's document keeps calling it without `--detach`: a detached start's document
carries no `mergeCommitSha`, and its outcome is only in the handle.

## The document

The document of the merge of [#2368](https://github.com/giantswarm/devctl/pull/2368), shortened where `…` stands:

```json
{
  "command": "pr merge",
  "schemaVersion": 1,
  "exitCode": 0,
  "verdict": "green",
  "reason": "",
  "warnings": [],
  "startedAt": "2026-09-23T15:04:35.605549435Z",
  "finishedAt": "2026-09-23T15:10:55.718102175Z",
  "repository": "giantswarm/devctl",
  "number": 2368,
  "headSha": "aa51d062e5ba36e883ed04f7a8e8a409dcb77ebf",
  "baseRef": "main",
  "checks": [
    {"name": "ci/circleci: go-build", "source": "status", "status": "completed", "conclusion": "success",
     "url": "https://circleci.com/gh/giantswarm/devctl/17836", "required": true},
    …
  ],
  "circleci": {
    "pipelineId": "…", "pipelineNumber": 8913,
    "workflows": [{"name": "build", "status": "success", "url": "https://app.circleci.com/pipelines/github/giantswarm/devctl/8913/workflows/…"}, …]
  },
  "actions": [],
  "mergeCommitSha": "c0621f88454b753c64ab264b2e39a9bcf15873c7",
  "mergedBy": "teemow",
  "method": "squash",
  "branchDeleted": true,
  "enqueued": false,
  "release": {
    "verdict": "available",
    "reason": "",
    "repository": "giantswarm/devctl",
    "tag": "v8.91.0",
    "sha": "c0621f88454b753c64ab264b2e39a9bcf15873c7",
    "releaseModel": "auto-release",
    "ciModel": "generated",
    "artifacts": [
      {"kind": "image", "reference": "gsoci.azurecr.io/giantswarm/devctl:8.91.0",
       "digest": "sha256:3acb07a67e1a1f017ddeb8ad33d58c13f14bfc19aec25d2459704ad441331916", "state": "available"}
    ],
    "pipeline": {"id": "…", "number": 8918, "url": "https://app.circleci.com/pipelines/github/giantswarm/devctl/8918",
                 "workflows": [{"name": "build", "status": "success"}, {"name": "setup", "status": "success"}],
                 "failedJobs": [], "unfinished": []},
    "actions": []
  },
  "dispatch": null
}
```

The envelope and the fields from `repository` to `unfinished[]` are [`pr wait`'s](pr-wait.md#the-document).
`pr merge` adds:

| Field | Meaning |
|---|---|
| `mergeCommitSha` | The commit the merge produced (the squash commit, the rebased head, or the queue's merge commit); empty when nothing merged. |
| `mergedBy` | The login the merge was made as: the account the token acts as, or for a merge queue the account GitHub records as the merger. Empty when nothing merged. |
| `method` | `squash` or `rebase`, as asked. |
| `branchDeleted` | The head branch was deleted after the merge, or was gone already. `false` when nothing merged and for a head in a fork. |
| `enqueued` | The base has a merge queue and the pull request went through it. |
| `unansweredReviews` | The feedback the last review read found unanswered, each with `kind` (`review`, `review_comment`, `comment`), `login`, `state` (reviews only), `at` and `url`; empty when nothing held the merge, `null` when a refusal came before the read. |
| `release` | The release the merge triggered, as `devctl release wait --pr` reports it: `verdict` (`available`, `no_release`, `ci_failed`, `timeout`, `not_applicable`, `usage`, `auth_required`), `reason`, and its result fields (`repository`, `tag`, `sha`, `releaseModel`, `ciModel`, `artifacts[]`, `pipeline`, `actions[]`, see [release-wait.md](release-wait.md#the-document)). `null` with `--no-release-wait` and when nothing merged. |
| `dispatch` | The workflow dispatched after the merge ([above](#after-the-merge-a-workflow-dispatch)): `workflow` (`<owner>/<repo>/<file>`), `ref`, `inputs` (`repository`, `pull_request`, `release`), `dispatched` and, when GitHub refused it, `reason`. `null` without `--dispatch` (or `DEVCTL_MERGE_DISPATCH`) and when nothing merged. |

## Exit codes

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `green` | Merged (or enqueued and merged by the queue), the branch deleted, and the release is pullable or none follows the merge (`release.verdict` says which). |
| 1 | `red` | Something failed during the wait, or the merge queue dropped the pull request; `reason` names it. |
| 2 | `timeout` | The timeout passed before an outcome, in the wait or in the queue; `unfinished` names what was still open. |
| 3 | `not_applicable` | Draft, closed, merged, conflicting, behind a strict base without `--update-branch`, or GitHub declined the merge as the pull request stands; `reason` says which. |
| 4 | `required_missing`, `approval_required` | A required status context never reported, or the head waits only for Actions runs awaiting a member's approval (`approval_required`), known at the poll that saw it and before any merge; `reason` names the context or the runs. |
| 5 | `refused` | Another human's pull request, a repository whose entry says `agentMerge: false`, or an unanswered review; `reason` names the author, the field or the feedback (`unanswered review: …`, listed in `unansweredReviews`). |
| 6 | `release_failed` | **Merged**, and the release failed: the merge commit's auto-release run failed before it tagged, or the tag pipeline failed; `release.pipeline.failedJobs` names the jobs. |
| 7 | `usage` | Wrong arguments or flags, a newer devctl released (the reason names `devctl version update`), or a tooling failure (GitHub or CircleCI answered with an error other than a 5xx, or a read failed eight tries in a row; the team files could not be read). |
| 8 | `auth_required` | No usable token; `reason` names the `devctl auth login` to run. |
| 9 | `release_unconfirmed` | **Merged**, and the release was not confirmed pullable: `--release-timeout` passed, the release wait could not judge it, or the auto-release run was superseded; `release.verdict` and `reason` say which. |

`--detach` exits 0 with verdict `detached` once the merge started, or with the code of the check
that refused the call. `devctl pr merge status` exits with the merge's code once it ended, 10
(`running`) while it runs, and 7 for a lost merge or an unknown handle.

Codes 1 to 5 mean nothing was merged, and 6 and 9 that the pull request was merged. A 7 or 8 is read
with `mergeCommitSha`: a failure after the merge (deleting the branch) leaves it set, while the release
wait's own tooling and authentication failures are 9.

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `DEVCTL_GITHUB_API_URL` | `https://api.github.com` | The GitHub REST API; the GraphQL endpoint is `<root>/graphql`. |
| `DEVCTL_CIRCLECI_API_URL` | `https://circleci.com/api/v2` | The CircleCI API v2. |
| `DEVCTL_REGISTRY_PUBLIC`, `DEVCTL_REGISTRY_PRIVATE`, `DEVCTL_REGISTRY_INSECURE` | as in [release-wait.md](release-wait.md#environment) | The registries the release's artifacts are probed in. |
| `DEVCTL_MERGE_DISPATCH` | unset | The workflow every merge dispatches afterwards, `<owner>/<repo>/<workflow file>[@<ref>]`, the value of `--dispatch`, which wins over it ([above](#after-the-merge-a-workflow-dispatch)). |
| `DEVCTL_KEYRING_FILE` | unset | A 0600 JSON file in place of the OS keychain (tests). |
| `DEVCTL_TIME_SCALE` | `1` | Multiplies every poll interval and both timeouts (tests). |
