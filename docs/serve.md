# devctl serve

```
devctl serve [--address :8080] [--progress-interval 10s] [--release-promote]
```

Serves the agent-facing commands as MCP tools over streamable HTTP, at `/mcp` (`/healthz` answers 200). A platform runs
it behind its MCP gateway (muster), and an agent calls the tools as the person who invoked it. The laptop keeps the CLI;
the server speaks no stdio.

## Tools

| Tool | Command | Arguments | Read-only |
|---|---|---|---|
| `pr_wait` | [`devctl pr wait`](pr-wait.md) | `repository` (owner/repo), `number`, `timeout` | yes |
| `pr_merge` | [`devctl pr merge`](pr-merge.md) | `repository`, `number`, `timeout`, `releaseTimeout`, `noReleaseWait`, `method` (squash or rebase), `updateBranch`, `acceptRed` | no |
| `pr_merge_status` | [`devctl pr merge status`](pr-merge.md) | `handle` (empty lists every merge) | yes |
| `release_wait` | [`devctl release wait`](release-wait.md) | `repository`, `version` or `pr`, `timeout`, `catalog`, `images`, `charts` | yes |
| `release_promote` (`--release-promote` only) | [`devctl release promote`](release-promote.md) | `repositories` or `team`, `dryRun`, `version`, `branch`, `noFollow`, `followTimeout` | no |

`timeout` is a Go duration, 30m by default and at most 55m, below the hour a gateway keeps a progress-kept call alive.
`pr_merge_status` reads the merges this server runs, `pr_merge`'s, kept in memory; a laptop's detached merges stay on
the laptop.

## pr_merge

`pr_merge` is `devctl pr merge` in one call, kept alive by progress like a wait: the wait for green, the merge with the
judged head as the expected head, the branch deletion, then the release wait on the merge commit. It refuses what the
CLI refuses, with the same document: a draft, closed or conflicting pull request (exit 3), another human's (exit 5), a
repository whose team-file entry says `agentMerge: false` (exit 5), `acceptRed` naming a required check or a security
scan (exit 3). These refusals guide an agent; GitHub's branch protection and rulesets are the boundary, for `pr_merge`
and `gh pr merge` alike.

A call on a pull request merged already answers that merge (its `mergeCommitSha` and `mergedBy`, a warning that no
merge call was made) and waits for its release, where the CLI exits 3: an agent that lost an answer calls again and
nothing merges twice. A second call while one runs waits for it. After a merge, "merged, release failed" stays exit 6
and "merged, release not confirmed" exit 9. `timeout` and `releaseTimeout` together stay within 55m; `releaseTimeout`
defaults to the rest, at most 30m. There is no `dispatch` and no failed-log tail (a CircleCI read).

## Answers

A tool answers the command's JSON document as structured content, and the same JSON as text: devctl's envelope
(`command`, `schemaVersion`, `exitCode`, `verdict`, `reason`, `warnings`, `startedAt`, `finishedAt`) and the command's
fields, as the CLI prints them for the same state. The exit code is a field, never an MCP error, except for
`auth_required`: then the call is an MCP error (`isError`) carrying the same document.

## One call per wait

A wait blocks until the command's own verdict, as the CLI does, with the CLI's timeout. While it waits it sends
`notifications/progress` on the caller's `progressToken`, at once and then every `--progress-interval`: a `progress` that
increases and a `message` naming what it waits for and its latest poll. Nothing is sent after the verdict. muster
restarts the server's registered `timeout` on each notification of a call that carries a `progressToken`, up to
`spec.progress.maxDuration`, so `--progress-interval` stays below that `timeout`.

A wait called without a `progressToken` is refused at once and reads nothing: verdict `progress_token_required`,
exit 7.

## What only a laptop has

The server has no version gate (its deployment pins the version), reads no keychain, docker keychain or kubeconfig,
keeps nothing under `$XDG_STATE_HOME`, does not ignore SIGHUP (SIGTERM drains the running calls for 30s) and runs its
waits at scale 1 whatever `DEVCTL_TIME_SCALE` says.

## Credentials

The server acts on GitHub with the token of the person a call is made for; with no token for them a call is
`auth_required`. A promotion's document names the identity source `platform`. The private registry is read
anonymously.

## CircleCI through GitHub

The server holds no CircleCI credential; the laptop keeps its own. `pr_wait`, `pr_merge` and `release_wait` read
CircleCI only through the commit statuses it posts to GitHub (`ci/circleci: <job>`). A job behind `requires:` posts
nothing until the jobs it requires have finished, so the waits read the jobs CircleCI runs from the revision's
`.circleci/config.yml` through GitHub: devctl's generated setup workflow continues with `.circleci/workflows.yml`
deep-merged with `.circleci/custom.yml`, any other configuration is its own; a job runs when its `filters` take the
pull request's branch (`pull/<n>` for a fork) or the tag and every job it requires runs, an approval posts nothing,
and a workflow runs when its `when`/`unless` holds for a push (pipeline parameters at their defaults, `trigger_source`
`webhook`), a scheduled one never. Every such job that has posted no status yet keeps a wait pending, named
`ci/circleci: <job> (not posted yet)`. What the configuration cannot tell before the pipeline runs (a matrix job, a
condition on another pipeline value, a setup workflow devctl does not generate) is a warning, and those jobs count once
they post.

`release_wait` judges the tag's CI by those statuses on the tag's commit: green once every tag job posted, no job is
pending and the newest status of no context failed; the document's `pipeline` lists the jobs, with no CircleCI id,
number or url. A job the branch's pipeline runs too may have posted from that pipeline; the tag-only jobs require it,
so they post only once it ran in the tag pipeline.

There is no `pr_rerun`, `ci_jobs`, `ci_rerun` or `release_rerun`, and no failed-log tail: they read or write CircleCI.
A coding agent that meets a flaky job reports it, or pushes a commit that rebuilds.
