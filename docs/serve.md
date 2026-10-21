# devctl serve

```
devctl serve [--address :8080] [--progress-interval 10s] [--release-promote]
             [--grant-app <app>] [--github-app <name>]
             [--oidc-issuer <url> --oidc-audience <aud>... [--oidc-ca-file <pem>]]
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

A wait called without a `progressToken` is refused at once and reads nothing: verdict `progress_token_required`,
exit 7.

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

- No grant: `auth_required` with `status: grant_required` and `apps: [<app>]` in the structured content, which muster
  answers with the App's sign-in link (`authUrl`); after the person signed in, the same call acts as them.
- A repository whose owner the App is not installed on (`GET /user/installations` with the person's token, kept five
  minutes): `not_applicable`, exit 3, naming the owner and the App (`--github-app`). Such a repository is for devctl on
  a laptop.

With `--oidc-issuer` every request carries the ID token muster forwards (`spec.auth.forwardToken`): signed by the issuer
(`--oidc-ca-file` for a private CA) for one of the `--oidc-audience` audiences, or the request is refused with 401
before any tool runs. Its subject is the caller of the call.

A promotion's document names the identity source `platform`. It holds no CircleCI credential: `pr_wait` judges CircleCI from the commit statuses it posts to GitHub,
and a release whose tag pipeline runs on CircleCI is not judged by `release_wait` yet (exit 7, the reason says so). The
private registry is read anonymously.

## Tracing

Each call logs one JSON line on stderr — `tool`, the caller's `subject` and `email`, `repository`, `verdict`,
`exitCode`, `duration` — and, with `--metrics-address`, counts it at `/metrics`:
`devctl_mcp_tool_calls_total` and `devctl_mcp_tool_call_duration_seconds` by `tool`, `subject`, `repository` and
`verdict`. A metric's `repository` is a GitHub `owner/repo` of a call past its usage checks, else empty; the email is
in the log only.

## Deployment

The chart `helm/devctl` runs the server from the image CircleCI pushes on every tag, so the server and the binary share
a version (its MCP `serverInfo.version`). One values change registers it with muster: `muster.mcpServer.enabled` with
`github.grantApp` and `identity.issuer` / `identity.audiences` (and `identity.caSecret` for a private CA) renders one
`MCPServer` with `toolPrefix: devctl` — tools `x_devctl_<tool>` — unpinned, `forwardToken: true`,
`providerApps: [<grantApp>]`, `timeout` 30 s and `progress.maxDuration` 3600 s. It runs one replica: a merge lives in
the memory of the replica that runs it.

An installation's muster configuration adds the toolset presets `devctl` (`server: devctl`, every tool, the write
tools included) and `devctl-read-only` (the waits, `pr_merge_status` and `reservation_list` by name), which resolves to
the same devctl tools as muster's built-in `read-only`.
