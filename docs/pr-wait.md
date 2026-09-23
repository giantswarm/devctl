# Waiting for a pull request's CI: `devctl pr wait`

```nohighlight
devctl pr wait <owner/repo> <number> [--timeout 30m] [--progress]
```

One blocking call that returns when the outcome of a pull request's CI is known: green, red, or a
state no CI can turn green. It prints one JSON document on stdout at the end and nothing else; the
exit code says what happened, so a script or an agent branches on it without parsing. `--progress`
writes one line per poll to stderr for a person watching.

The tokens come from the OS keychain (`devctl auth login`, see [auth.md](auth.md)). The GitHub
token is required before the first request; the CircleCI token only once the head turns out to
carry a CircleCI configuration for a repository CircleCI builds.

## What green means

`gh pr checks` and `gh run watch` read the check list, and the check list reads green too early.
`pr wait` reads what the merge box reads, and where the merge box has no information yet, the
system that will produce it:

1. **Every check run and commit status of the head is complete and not failed**, the latest run
   per name. GitHub keeps every run of a check; a check that ran twice for the same head has a
   stale first run next to the current one. A pull request retitled after a failed title check
   has a failed run and a passed run of the same name, and only the newest counts, as in the
   merge box. A completed run whose conclusion is `action_required` (a deployment waiting for a
   reviewer) is unfinished, not red. `neutral` and `skipped` pass.
2. **Every CircleCI workflow of the head revision is `success`**, read from CircleCI's API: the
   newest pipeline for the revision, the newest run of each workflow name in it. A CircleCI job
   behind `requires:` posts no status to GitHub until it starts, so a multi-stage pipeline reads
   green on GitHub between its stages; CircleCI knows the workflow is `running`. A rerun of a
   failed workflow is a new workflow of the same name in the same pipeline, and the newest run is
   the one that counts. `not_run` is a skipped workflow. An `errored` pipeline (a configuration
   error, which produces no workflows) is red.
3. **No GitHub Actions run of the head is open**: `queued`, `in_progress`, `waiting`, `pending`,
   `requested`, or completed with the conclusion `action_required`. The last one is a fork's
   workflow run awaiting a maintainer's approval: it completes at once, produces no check runs,
   and the check list is simply shorter than it will be. The wait goes on; at the timeout the run
   is named.
4. **Every required status context has reported**: the required status checks of the base
   branch's protection and of every ruleset in effect on it. A context nothing has reported under
   is unfinished. While anything of rules 1 to 3 is still pending, the wait goes on and the timeout
   is exit 2 whatever is absent: the run awaiting approval, the queued check or the running
   workflow may be what reports the context. Once every check, status, run and workflow of the
   head has finished and a required context is still absent, nothing is left that could report it:
   exit 4 at that poll, without waiting for the timeout.

Any failure anywhere is red at once (exit 1): a check run concluded `failure`, `timed_out`,
`cancelled`, `startup_failure` or `stale`, a status `failure` or `error`, a CircleCI workflow
`failed`, `error`, `failing`, `canceled` or `unauthorized`, an `errored` pipeline.

A head that reports nothing at all is green: a repository without CI merges on review alone. The
required contexts of a protected base are what make such a head wait.

## What ends the wait before it starts

Exit 3, `not_applicable`, for a pull request that cannot become green as it is: it is a **draft**;
it is **closed** or **merged**; it **conflicts** with its base (GitHub's `mergeable_state` is
`dirty`), which is why no `pull_request` workflow runs for it and a check-list loop waits until it
gives up; it is **behind** a base whose protection requires branches to be up to date (`behind`).
A mergeable state GitHub has not computed yet (`unknown`) is none of these; the wait goes on and
reads the pull request again on the next poll.

The pull request is re-read on every poll. A head that changes under the wait (a new push) resets
the wait to the new head and adds a warning to the document.

## GitHub alone or GitHub and CircleCI

CircleCI is part of the verdict when the head carries `.circleci/config.yml` and CircleCI has a
project for the repository. A repository with neither, an upstream fork being contributed to, is
judged from GitHub alone and needs only the GitHub token. A head with the configuration file but no
CircleCI project is judged from GitHub alone with a warning in the document. A pull request from a
fork of a CircleCI-built repository is looked up under the branch CircleCI gives it, `pull/<number>`.

## Polling

Every GitHub request is conditional: the ETag of the last answer goes out as `If-None-Match`, and
a `304 Not Modified` costs nothing against the rate limit. The interval between polls comes from
the `X-RateLimit-Remaining` and `X-RateLimit-Reset` headers of the answers themselves (never from
the rate-limit endpoint): the remaining budget spread over the time to its reset, bounded to 15 s
at the shortest and 60 s at the longest. `DEVCTL_TIME_SCALE` multiplies every interval and the
timeout (the e2e suite runs at 0.001).

A read GitHub or CircleCI does not answer is not an outcome: every poll reads the same state again, so
a reset connection, an EOF, a try that takes longer than 60 s or a 5xx is sent again after 2 s, the
pause doubling up to 60 s, for up to eight tries in a row (about three minutes of pauses), never past
the timeout. Each retried failure is a warning with its time:

```
2026-09-23T10:11:12Z GET https://api.github.com/repos/giantswarm/devctl/pulls/2360: 500 Internal Server Error; retried in 2s (try 2 of 8)
```

A read that fails all eight tries ends the wait with exit 7, the reason naming the request, the count
and the last failure (`Get "https://api.github.com/repos/giantswarm/devctl/pulls/2360": failed 8 times in
a row: 500 Internal Server Error`). A 4xx is an answer and is never retried, nor is a certificate the
client refuses or a host that does not exist; only reads (GET, HEAD) are retried.

## The document

```json
{
  "command": "pr wait",
  "schemaVersion": 1,
  "exitCode": 0,
  "verdict": "green",
  "reason": "",
  "warnings": [],
  "startedAt": "2026-09-21T10:00:00Z",
  "finishedAt": "2026-09-21T10:04:31Z",
  "repository": "giantswarm/devctl",
  "number": 2277,
  "headSha": "6a2df08b…",
  "baseRef": "main",
  "checks": [
    {"name": "go-build", "source": "check_run", "status": "completed", "conclusion": "success",
     "url": "https://github.com/giantswarm/devctl/runs/…", "required": true},
    {"name": "ci/circleci: test", "source": "status", "status": "completed", "conclusion": "success",
     "url": "https://circleci.com/gh/giantswarm/devctl/…", "required": false}
  ],
  "circleci": {
    "pipelineId": "…", "pipelineNumber": 4123,
    "workflows": [{"name": "build", "status": "success", "url": "https://app.circleci.com/pipelines/github/giantswarm/devctl/4123/workflows/…"}]
  },
  "actions": [
    {"name": "PR title", "runId": 1234567, "status": "completed", "conclusion": "success", "url": "https://github.com/giantswarm/devctl/actions/runs/1234567"}
  ]
}
```

| Field | Meaning |
|---|---|
| `command`, `schemaVersion`, `exitCode`, `verdict`, `reason`, `warnings`, `startedAt`, `finishedAt` | The envelope every agent-facing command prints. `verdict` is `green`, `red`, `timeout`, `not_applicable`, `required_missing`, `auth_required` or `usage`; `reason` is one sentence for anything but green; `warnings` carries the CircleCI token's expiry notice, a head change, a missing CircleCI project, each retried read (Polling). |
| `repository`, `number` | The pull request as given. |
| `headSha`, `baseRef` | The head commit judged and the base branch whose protection was read. |
| `checks[]` | The head's check runs and statuses, the latest per name, sorted by name. `source` is `check_run` or `status`; `status` is `queued`, `in_progress` or `completed` for a check run and `pending` or `completed` for a status; `conclusion` is the check run's conclusion or the status's state, empty while unfinished; `required` says whether the base requires this context. |
| `circleci` | Present only when CircleCI was consulted: the newest pipeline of the head revision and its workflows, the newest run per name, sorted by name. |
| `actions[]` | The head's GitHub Actions runs, the latest per workflow name, sorted by name. |
| `unfinished[]` | Present at exit 2 and 4: what the head was still waiting for, one line each (`check go-test (in_progress)`, `circleci workflow build (running)`, `actions run CI (awaiting approval)`, `required context lint (absent)`). |

## Exit codes

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `green` | Every rule above holds. |
| 1 | `red` | Something failed; `reason` names every failed check, status, run and workflow. |
| 2 | `timeout` | The timeout passed before an outcome; `unfinished` names what was still open, a required context still absent among it. |
| 3 | `not_applicable` | Draft, closed, merged, conflicting or behind a strict base; `reason` says which. |
| 4 | `required_missing` | Every check, run and workflow of the head has finished and a required status context never reported; `reason` names it. Known at the poll that saw it, before the timeout; with anything still pending the outcome is 2, not 4. |
| 7 | `usage` | Wrong arguments, or a tooling failure: GitHub or CircleCI answered with an error other than a 5xx, or a read failed eight tries in a row (Polling). |
| 8 | `auth_required` | No usable token; `reason` names the `devctl auth login` to run. |

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `DEVCTL_GITHUB_API_URL` | `https://api.github.com` | The GitHub REST API. |
| `DEVCTL_CIRCLECI_API_URL` | `https://circleci.com/api/v2` | The CircleCI API v2. |
| `DEVCTL_KEYRING_FILE` | unset | A 0600 JSON file in place of the OS keychain (tests). |
| `DEVCTL_TIME_SCALE` | `1` | Multiplies every poll interval and the timeout (tests). |
