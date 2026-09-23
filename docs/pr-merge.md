# Merging a pull request: `devctl pr merge`

```nohighlight
devctl pr merge <owner/repo> <number> [--timeout 30m] [--release-timeout 30m] [--no-release-wait]
                [--rebase] [--update-branch] [--progress]
```

One blocking call that waits until the pull request's head is green (the wait of
[`devctl pr wait`](pr-wait.md), verdicts and codes included), merges it through the merge API,
deletes its branch through the refs API, and then waits until the release the merge triggers is
pullable (the wait of [`devctl release wait --pr`](release-wait.md)). It prints one JSON document on
stdout at the end and nothing else; the exit code says what happened. `--progress` writes one line
per step to stderr.

An agent that merges its own pull request makes this one call and is done when it exits 0: CI was
green, the pull request is merged, and the release is pullable or none follows the merge. Nothing
before a consumer bump or a rollout needs a second wait.

The tokens come from the OS keychain (`devctl auth login`, see [auth.md](auth.md)): the GitHub token
before the first request, the CircleCI token only once the head or the tag turns out to carry a
CircleCI configuration for a repository CircleCI builds, as in `pr wait` and `release wait`.

## What is refused before the wait

Every refusal comes before the first poll and before anything is written.

| Code | Verdict | Refused |
|---|---|---|
| 3 | `not_applicable` | A **draft**; a **closed** or **merged** pull request; one that **conflicts** with its base; one **behind** a base whose protection requires branches to be up to date, unless `--update-branch` is given. The rule is `pr wait`'s. |
| 5 | `refused` | A pull request **another human opened**. The author is compared with the account the token acts as (the login of the keychain record, or `GET /user`); a bot, a GitHub App (user type `Bot`) or a `[bot]` login is fine, so a Renovate or Dependabot pull request merges. Giant Swarm's automation accounts that GitHub carries as plain users are fine too -- `taylorbot`, which opens the release pull request of every repository on devctl-generated CI, and `architectbot` -- each matched on its numeric account id as well as its login, so the login alone opens nothing. The reason names every author the merge accepts, and for an automation login whose account id is not the pinned one, both ids. |
| 5 | `refused` | A repository whose team-file entry **opts out of agent merges**: the entry named after the repository in `repositories/<team>.yaml` of `giantswarm/github` at `main` says `agentMerge: false`. The entry is read the way `devctl repo` reads one; the reason names the field. An entry without the field, a repository no team file declares and a repository outside the organisation the team files declare are not opted out. Team files the token cannot read are a tooling failure (7), not a guess. |

A mergeable state GitHub has not computed yet (`unknown`) is read again a few times before the
refusals are applied, so a pull request just pushed is judged on its state and not on GitHub's
laziness.

## The wait

The wait is `pr wait`'s, with its verdicts and exit codes: 1 red, 2 timeout, 3 not applicable, 4 a
required context never reported. Nothing is merged on any of them. A head that changes under the
wait resets it to the new head with a warning.

## The merge

Green lands with one call of the merge API, `PUT /repos/{owner}/{repo}/pulls/{number}/merge`:

- **Squash** by default; `--rebase` is a rebase merge, for repositories whose convention is one
  commit per patch (the upstream-line forks). Never a merge commit.
- The judged head SHA goes along as the expected head: a head that moved between the verdict and
  the merge is not merged (GitHub answers 409), and the pull request is re-read after the wait for
  the same reason (a retitle during the wait names the squash commit).
- The squash commit's subject is the pull request's title with its number, `<title> (#<number>)`:
  the title the title check accepted is what the auto-release reads.
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
strict protection; 409: the head moved) is exit 3, `not_applicable`, with GitHub's sentence as the
reason. Declined for the review rule, the reason goes on to say whom devctl acted as, which rulesets
of the base carry a pull request rule and their bypass actors (read, never written; an App actor is
marked as covering installation tokens only), and the team whose file declares the entry: one of its
members or a repository admin merges it, or a reviewer with write access approves it first. A base whose review requirement
no ruleset carries is on classic branch protection: the repository is aligned first, never merged
past it.

### A base with a merge queue

When the rules in effect on the base (`GET /repos/{owner}/{repo}/rules/branches/{base}`) include a
`merge_queue` rule, the merge API is not the way: the pull request is **enqueued** (the GraphQL
mutation `enqueuePullRequest`; REST has none), the command waits until the queue merged it, within
the same `--timeout`, then deletes the branch. `enqueued` is true in the document and
`mergeCommitSha` is the commit the queue produced. A pull request the queue drops (closed without a
merge) is exit 1; the deadline is exit 2 with `unfinished` naming the queue.

### `--update-branch`

A head behind a base that requires branches to be up to date cannot merge as it is. With
`--update-branch` the command asks GitHub to merge the base into the head (the **Update branch**
button, `PUT .../pulls/{number}/update-branch` with the current head as the expected head), reads
the pull request until the new head is on it, and the wait judges that head: the CI of the updated
branch is what turns green, and the merge lands it. Without the flag, behind is exit 3.

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

## The document

```json
{
  "command": "pr merge",
  "schemaVersion": 1,
  "exitCode": 0,
  "verdict": "green",
  "reason": "",
  "warnings": [],
  "startedAt": "2026-09-21T10:00:00Z",
  "finishedAt": "2026-09-21T10:04:40Z",
  "repository": "giantswarm/devctl",
  "number": 2278,
  "headSha": "6a2df08b…",
  "baseRef": "main",
  "checks": [
    {"name": "go-build", "source": "check_run", "status": "completed", "conclusion": "success",
     "url": "https://github.com/giantswarm/devctl/runs/…", "required": true}
  ],
  "circleci": {
    "pipelineId": "…", "pipelineNumber": 4123,
    "workflows": [{"name": "build", "status": "success", "url": "https://app.circleci.com/pipelines/github/giantswarm/devctl/4123/workflows/…"}]
  },
  "actions": [],
  "mergeCommitSha": "0f3a9c1d…",
  "method": "squash",
  "branchDeleted": true,
  "enqueued": false,
  "release": {
    "verdict": "available",
    "reason": "",
    "repository": "giantswarm/devctl",
    "tag": "v8.91.0",
    "sha": "0f3a9c1d…",
    "releaseModel": "auto-release",
    "ciModel": "generated",
    "artifacts": [
      {"kind": "release-asset", "reference": "https://github.com/giantswarm/devctl/releases/download/v8.91.0/devctl-v8.91.0-linux-amd64.tar.gz",
       "digest": "sha256:…", "state": "available"}
    ],
    "pipeline": {"id": "…", "number": 4130, "url": "https://app.circleci.com/pipelines/github/giantswarm/devctl/4130",
                 "workflows": [{"name": "build", "status": "success"}], "failedJobs": [], "unfinished": []},
    "actions": []
  }
}
```

The envelope and the fields from `repository` to `unfinished[]` are [`pr wait`'s](pr-wait.md#the-document).
`pr merge` adds:

| Field | Meaning |
|---|---|
| `mergeCommitSha` | The commit the merge produced (the squash commit, the rebased head, or the queue's merge commit); empty when nothing merged. |
| `method` | `squash` or `rebase`, as asked. |
| `branchDeleted` | The head branch was deleted after the merge, or was gone already. `false` when nothing merged and for a head in a fork. |
| `enqueued` | The base has a merge queue and the pull request went through it. |
| `release` | The release the merge triggered, as `devctl release wait --pr` reports it: `verdict` (`available`, `no_release`, `ci_failed`, `timeout`, `not_applicable`, `usage`, `auth_required`), `reason`, and its result fields (`repository`, `tag`, `sha`, `releaseModel`, `ciModel`, `artifacts[]`, `pipeline`, `actions[]`, see [release-wait.md](release-wait.md#the-document)). `null` with `--no-release-wait` and when nothing merged. |

## Exit codes

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `green` | Merged (or enqueued and merged by the queue), the branch deleted, and the release is pullable or none follows the merge (`release.verdict` says which). |
| 1 | `red` | Something failed during the wait, or the merge queue dropped the pull request; `reason` names it. |
| 2 | `timeout` | The timeout passed before an outcome, in the wait or in the queue; `unfinished` names what was still open. |
| 3 | `not_applicable` | Draft, closed, merged, conflicting, behind a strict base without `--update-branch`, or GitHub declined the merge as the pull request stands; `reason` says which. |
| 4 | `required_missing` | A required status context never reported within the timeout; `reason` names it. |
| 5 | `refused` | Another human's pull request, or a repository whose entry says `agentMerge: false`; `reason` names the author or the field. |
| 6 | `release_failed` | **Merged**, and the release failed: the merge commit's auto-release run failed before it tagged, or the tag pipeline failed; `release.pipeline.failedJobs` names the jobs. |
| 7 | `usage` | Wrong arguments, or a tooling failure (GitHub or CircleCI answered with an error; the team files could not be read). |
| 8 | `auth_required` | No usable token; `reason` names the `devctl auth login` to run. |
| 9 | `release_unconfirmed` | **Merged**, and the release was not confirmed pullable: `--release-timeout` passed, the release wait could not judge it, or the auto-release run was superseded; `release.verdict` and `reason` say which. |

Codes 1 to 5 mean nothing was merged, and 6 and 9 that the pull request was merged. A 7 or 8 is read
with `mergeCommitSha`: a failure after the merge (deleting the branch) leaves it set, while the release
wait's own tooling and authentication failures are 9.

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `DEVCTL_GITHUB_API_URL` | `https://api.github.com` | The GitHub REST API; the GraphQL endpoint is `<root>/graphql`. |
| `DEVCTL_CIRCLECI_API_URL` | `https://circleci.com/api/v2` | The CircleCI API v2. |
| `DEVCTL_REGISTRY_PUBLIC`, `DEVCTL_REGISTRY_PRIVATE`, `DEVCTL_REGISTRY_INSECURE` | as in [release-wait.md](release-wait.md#environment) | The registries the release's artifacts are probed in. |
| `DEVCTL_KEYRING_FILE` | unset | A 0600 JSON file in place of the OS keychain (tests). |
| `DEVCTL_TIME_SCALE` | `1` | Multiplies every poll interval and both timeouts (tests). |
