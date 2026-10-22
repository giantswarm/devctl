# devctl serve

```
devctl serve [--address :8080] [--progress-interval 10s] [--no-progress-timeout 20s] [--release-promote]
             [--grant-app <app>] [--github-app <name>]
             [--metrics-address :9090]
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

## A call without a progressToken

Not every client sends a `progressToken`: a tool called through muster's `call_tool` meta-tool, or with `muster call`,
may carry none. Nothing keeps such a call alive past the gateway's registered `timeout`, so a blocking tool (`pr_wait`,
`pr_merge`, `release_wait`, `release_promote`) answers it within `--no-progress-timeout` (20s, below muster's 30 s)
instead of blocking until its verdict, and sends no progress:

- `pr_wait` and `release_wait` run their wait for at most that long, whatever `timeout` says, and answer the verdict
  they reached: `green` or `available` when it came in time, `red`, `ci_failed` or any other verdict as it fell, and
  `timeout` (exit 2), its reason naming what is still pending, while the head or the release is not done yet.
- `pr_merge` shares that time between the wait for green and the release wait. A head it has not judged green by then
  is not merged (`timeout`, exit 2). A merged one whose release is not confirmed in the time left is
  `release_unconfirmed` (exit 9); another call answers the merge made already and waits for the release again, with no
  second merge. A call while another merges the same pull request answers `timeout` at once.
- `release_promote` dispatches as with a `progressToken` and follows the runs for at most that long.

Every such answer carries a warning that the call had no `progressToken`. The caller calls the tool again until the
verdict is no longer `timeout` (or `release_unconfirmed` after a merge); a call with a `progressToken` waits for the
verdict in one call. Without a grant the call is `auth_required` as with a token.

## What only a laptop has

The server has no version gate (its deployment pins the version), reads no keychain, docker keychain or kubeconfig,
keeps nothing under `$XDG_STATE_HOME`, does not ignore SIGHUP (SIGTERM drains the running calls for 30s) and runs its
waits at scale 1 whatever `DEVCTL_TIME_SCALE` says.

## Credentials

The server acts on GitHub with the token of the person a call is made for; with no token for them a call is
`auth_required`. That token is the person's grant of the GitHub App muster keeps as `--grant-app` (muster's
`aggregator.providerApps`; on a Giant Swarm installation the `<installation>-agents` App, the identity coding agents
use for git): muster attaches it to every call in `X-Muster-Grant-<app>` once the person granted the App, because the
server's `MCPServer` names the App in `spec.auth.providerApps`. The server stores no token and runs no sign-in.

- The grant names the caller: GitHub's user for the token (`GET /user`, kept five minutes), its login and id.
- No grant, or one GitHub refuses: every tool call, `pr_merge_status` included, is `auth_required` with
  `status: grant_required` and `apps: [<app>]` in the structured content, which muster answers with the App's sign-in
  link (`authUrl`); after the person signed in, the same call acts as them. Opening a session and listing the tools
  need no grant, so the person sees the tools before granting the App.
- A repository whose owner the App is not installed on (`GET /user/installations` with the person's token, kept five
  minutes): `not_applicable`, exit 3, naming the owner and the App (`--github-app`). Such a repository is for devctl on
  a laptop.

The server reads no ID token: its `MCPServer` keeps `spec.auth.forwardToken` only because muster attaches provider App
grants to a connection made as the person, so no list of audiences has to follow muster's.

A promotion's document names the identity source `platform`. The private registry is read anonymously.

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

## Tracing

Each call logs one JSON line on stderr — `tool`, the caller's GitHub `login` and `userId`, `repository`, `verdict`,
`exitCode`, `duration` — and, with `--metrics-address`, counts it at `/metrics`:
`devctl_mcp_tool_calls_total` and `devctl_mcp_tool_call_duration_seconds` by `tool`, `login`, `user_id`, `repository`
and `verdict`. A call refused before its grant named the caller (no grant, a usage error) is `login` `anonymous` with
an empty id. A metric's `repository` is a GitHub `owner/repo` of a call past its usage checks, else empty.

## Deployment

The chart `helm/devctl` runs the server from the image CircleCI pushes on every tag, so the server and the binary share
a version (its MCP `serverInfo.version`). One values change registers it with muster: `muster.mcpServer.enabled` with
`github.grantApp` renders one
`MCPServer` with `toolPrefix: devctl` — tools `x_devctl_<tool>` — unpinned, `forwardToken: true`,
`providerApps: [<grantApp>]`, `timeout` 30 s and `progress.maxDuration` 3600 s. It runs one replica: a merge lives in
the memory of the replica that runs it.

An installation's muster configuration adds the toolset presets `devctl` (`server: devctl`, every tool, the write
tools included) and `devctl-read-only` (`pr_wait`, `release_wait`, `rollout_wait`, `pr_merge_status` and
`reservation_list` by name), which resolves to the same devctl tools as muster's built-in `read-only`. A preset names
`rollout_wait` and `reservation_list` ahead of the server: a name no tool in the [list above](#tools) answers matches
nothing until a release serves it.
