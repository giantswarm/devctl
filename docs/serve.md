# devctl serve

```
devctl serve [--address :8080] [--progress-interval 10s]
```

Serves the agent-facing commands as MCP tools over streamable HTTP, at `/mcp` (`/healthz` answers 200). A platform runs
it behind its MCP gateway (muster), and an agent calls the tools as the person who invoked it. The laptop keeps the CLI;
the server speaks no stdio.

## Tools

| Tool | Command | Arguments | Read-only |
|---|---|---|---|
| `pr_wait` | [`devctl pr wait`](pr-wait.md) | `repository` (owner/repo), `number`, `timeout` | yes |
| `pr_merge_status` | [`devctl pr merge status`](pr-merge.md) | `handle` (empty lists every merge) | yes |
| `release_wait` | [`devctl release wait`](release-wait.md) | `repository`, `version` or `pr`, `timeout`, `catalog`, `images`, `charts` | yes |

`timeout` is a Go duration, 30m by default and at most 55m, below the hour a gateway keeps a progress-kept call alive.
`pr_merge_status` reads the merges this server runs, kept in memory; a laptop's detached merges stay on the laptop.

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
`auth_required`. It holds no CircleCI credential: `pr_wait` judges CircleCI from the commit statuses it posts to GitHub,
and a release whose tag pipeline runs on CircleCI is not judged by `release_wait` yet (exit 7, the reason says so). The
private registry is read anonymously.
