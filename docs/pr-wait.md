# Waiting for a pull request's CI: `devctl pr wait`

```nohighlight
devctl pr wait <owner/repo> <number> [--timeout 30m] [--progress] [--failed-log [--failed-log-lines 50]]
```

One blocking call that returns when the outcome of a pull request's CI is known: green, red, or a
state no CI can turn green. It prints one JSON document on stdout at the end and nothing else; the
exit code says what happened, so a script or an agent branches on it without parsing. `--progress`
writes one line per poll to stderr for a person watching.

`--failed-log` answers why a red wait is red without a second tool: on a red verdict it reads the
log of each failed job once, a GitHub Actions job (`GET /repos/{owner}/{repo}/actions/jobs/{id}/logs`)
and a CircleCI job (the output of its failed steps), and keeps the last `--failed-log-lines` lines
(default 50), without terminal control sequences or control characters but tab. An Actions log ends at its last `##[error]` line, the failed
step's end, so the post-job cleanup after it does not crowd out why the job failed. When the log's first failure
lies before those lines, up to `--failed-log-lines` lines from just before it lead the tail, the lines
skipped between them counted as `[... <n> lines ...]`: a step that failed early, or a failing Go package's `--- FAIL`
block, is not pushed out by the green output after it. A failure is the first line carrying `##[error]`,
`--- FAIL`, a leading `FAIL` (Go, Jest), `panic: `, Prettier's `[error]` or `Code style issues`, or a non-zero exit
code (`exit code 2`, `exited with code 1`), after an optional Actions timestamp and `[name] ` prefix; a failure
already within the tail, a single failing last step, prints the tail alone. The tails go to stderr, one block per job headed
`failed job <name> (<url>):`, and into the document under `failedJobs[]`. A green, pending or
timed-out wait reads no log and costs no extra request. A log that cannot be read (expired, not
yet uploaded) is named in its job's `logError`; the verdict stays red.

The tokens come from the OS keychain (`devctl auth login`, see [auth.md](auth.md)). The GitHub
token is required before the first request; the CircleCI token only once the head turns out to
carry a CircleCI configuration for a repository CircleCI builds. The GitHub identity follows the repository's owner: the
App login for giantswarm, where the devctl App is installed, and your own `gh` login (`gh auth token` of the
real `gh`, never a `gh` link to devctl, and no environment variable) for every other owner, with the same
semantics; `identity` in the document says which, `app` or `gh`. Without a `gh` login such a pull request is
exit 8 naming `gh auth login`; a repository neither identity can read is exit 7 naming the missing
installation and the read access your `gh` login lacks.

## What green means

`gh pr checks` and `gh run watch` read the check list, and the check list reads green too early.
`pr wait` reads what the merge box reads, and where the merge box has no information yet, the
system that will produce it:

1. **Every check run and commit status of the head is complete and not failed**: every run of a
   check name, the latest status per context. The jobs of a matrix, or of several workflows
   calling one reusable workflow, report under one name, and each run must pass: one red job
   among nine green ones is red, and the verdict names it by its workflow and check run id.
   GitHub keeps every run of a check; a workflow that ran twice for the same head has a stale
   first run next to the current one. A pull request retitled after a failed title check has a
   failed run and a passed run of the same workflow, and only the newest workflow run counts, as
   in the merge box. A failed check of an earlier run of a workflow (a reopened pull request, a rerun) does
   not count while the workflow's newest run is still open: the wait goes on and exits on that run's outcome.
   A completed run whose conclusion is `action_required` (a deployment waiting for a
   reviewer) is unfinished, not red. `neutral` and `skipped` pass.
   A pending check the base does not require is not waited for, as the merge box does not wait
   for it: a check run of an app other than GitHub Actions (a gatekeeper app that stays
   `in_progress` until the pull request is reviewed) and a status of another CI than CircleCI. It
   is named in `blocking[]` and a warning. A GitHub Actions job is still waited for through its
   run (rule 3), a CircleCI job through its pipeline (rule 2), and a failure of a check the base
   does not require is still red, once the required contexts have finished (see below).
2. **Every CircleCI workflow of the head revision is `success`**, read from CircleCI's API: the
   newest pipeline for the revision, the newest run of each workflow name in it. A CircleCI job
   behind `requires:` posts no status to GitHub until it starts, so a multi-stage pipeline reads
   green on GitHub between its stages; CircleCI knows the workflow is `running`. A rerun of a
   failed workflow is a new workflow of the same name in the same pipeline, and the newest run is
   the one that counts. `not_run` is a skipped workflow. An `errored` pipeline (a configuration
   error, which produces no workflows) is red. A setup pipeline (`setup: true` with
   `continuation/continue`) is unfinished while its state is `setup-pending`, `setup` or `pending`:
   its `setup` workflow may have finished, but the continued workflows that post the build's
   contexts do not exist yet. It settles at `created`, and reads `created` with the setup workflow
   alone for a minute or two after the setup job continued it, longer when CircleCI's listing lags
   behind the workflows it created; a listing that lags further reads `created` with no workflow at
   all. GitHub tells a fresh pipeline from a lagging listing: every continued job posts under its
   own `ci/circleci: <job>` context beside the setup job's, so a pipeline listed with its setup
   workflow alone (`setup finished, the continuation's workflows not created yet`) or with none
   (`no workflows yet`) is unfinished while the head carries one CircleCI context at most, and is
   judged by the contexts once more have posted, whatever the listing reads.
3. **No GitHub Actions run of the head is open**: `queued`, `in_progress`, `waiting`, `pending`,
   `requested`, or completed with the conclusion `action_required`. The last one is a workflow
   run awaiting a repository member's approval (a fork's or a bot's pull request in a repository
   that requires approval for such contributors): it completes at once, produces no check runs,
   and the check list is simply shorter than it will be. No wait starts it, a member's approval
   does: when such runs are all that is pending (nothing red, nothing else running, a required
   context absent or not), the wait ends at that poll with exit 4, `approval_required`, its
   `reason` naming each run with its URL. Once approved, the run starts again (`queued`,
   `in_progress`) and a new wait judges it like any other. A run that reads `in_progress` while
   every job of it the head lists has completed, the last one five minutes ago or longer, is no
   longer waited for: its jobs are its verdict (rule 1), and `actions[]` marks it `jobsCompleted`.
   GitHub has left a run `in_progress` for an hour after its only job succeeded; a job behind
   `needs:` is listed seconds after the jobs it needs complete, well inside the five minutes.
4. **Every required status context has reported**: the required status checks of the base
   branch's protection and of every ruleset in effect on it. A context nothing has reported under
   is unfinished, and so is one whose check run or status is registered and not completed. While
   anything of rules 1 to 3 is still pending, a pending check the base does not require included,
   the wait goes on and the timeout is exit 2 whatever is absent: the queued check, the running
   workflow or another app's pending check may be what reports the context; a run awaiting
   approval that is all that is pending is exit 4 `approval_required` (rule 3). Once every check,
   status, run and workflow of the head has finished, the registration grace has passed (below)
   and a required context is still absent, nothing is left that could report it: exit 4 at that
   poll, without waiting for the timeout.

Any failure is red (exit 1): a check run concluded `failure`, `timed_out`, `cancelled`,
`startup_failure` or `stale`, a status `failure` or `error`, a CircleCI workflow `failed`, `error`,
`failing`, `canceled` or `unauthorized`, an `errored` pipeline. It is red at once when the base
requires the failed context or nothing the base requires is still pending or absent. A failure the
base does not require (a check that has been red for days, say) waits for the required contexts
still running or yet to report, so the verdict carries their outcome too: red once they have
finished, at once if one of them fails. A timeout before they finish is exit 2, and its `reason`
names the failure seen so far.

**Registration grace.** GitHub registers a head's workflow runs seconds after the push, and after
the merge commit `pr merge --update-branch` creates. For one minute after the pull request's last
update, which such a push is, the wait gives no verdict on what has not registered yet: a required
context still absent is unfinished, not `required_missing`, and a head that carries no GitHub
Actions run and no check run of its own (another app's check, a gatekeeper that reports at once,
does not count) is unfinished as `actions runs of the head (none registered …)`, not green. A wait
started right after a push therefore blocks until the workflows have registered and finished. A
head that registers nothing at all, a repository without CI, is green once the minute has passed;
a required context that never reports is exit 4 then, with the wording above.

A head that reports nothing at all (no check run, no status, no Actions run, no CircleCI pipeline) is
green only when nothing is going to report: a repository without CI merges on review alone, and the
required contexts of a protected base are what make such a head wait. When the head's
`.github/workflows` hold an active workflow that every push to a pull request against the base runs
(`on: pull_request` without `paths` or `paths-ignore`, with `types` that include `opened` and
`synchronize`, and `branches` or `branches-ignore` the base passes), its runs are due: the head
waits for them (`actions runs of <workflow> (none registered)`). Two minutes after the pull
request's last update, which a push to its head is, with still nothing reported, GitHub did not
deliver the `pull_request` event and no wait makes it arrive: exit 4, `no_checks`, its `reason`
naming the workflows and the retrigger, closing and reopening the pull request. A head commit whose
message skips CI (`[skip ci]`, `[ci skip]`, `[no ci]`, `[skip actions]`, `[actions skip]`, a
`skip-checks: true` trailer) expects no run and stays green.

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
project for the repository. A repository without the file, an upstream fork being contributed to,
is judged from GitHub alone and needs only the GitHub token; so is, with a warning in the document,
a template repository (`is_template` on GitHub) whose team-file entry in giantswarm/github declares
`gen.ci.templateContent: true`, which carries the configuration for the repositories created from it
and is never built itself, and a repository CircleCI knows no project for. A template without that
declaration carries its own pipeline and is judged like any repository, as the reconciler judges it;
the entry is read only for a template whose head carries `.circleci/config.yml`. Whether the project has run a pipeline before does not decide it: the head's own pipeline may
be the project's first (a new repository's first pull request), and until it appears the head is
unfinished (`circleci pipeline for <sha> (absent)`), not green from GitHub alone. A pull request
from a fork of a CircleCI-built repository is looked up under the branch CircleCI gives it,
`pull/<number>`.

## Polling

Every GitHub request is conditional: the ETag of the last answer goes out as `If-None-Match`, and
a `304 Not Modified` costs nothing against the rate limit. The interval between polls comes from
the `X-RateLimit-Remaining` and `X-RateLimit-Reset` headers of the answers themselves (never from
the rate-limit endpoint): the remaining budget spread over the time to its reset, bounded to 15 s
at the shortest and 60 s at the longest. `DEVCTL_TIME_SCALE` multiplies every interval and the
timeout (the e2e suite runs at 0.001).

Without `--progress` the wait is silent but for a heartbeat on stderr: `waiting for <what>` once
every two minutes, and at once when what it waits for changed (`circleci workflow build (running)`,
`status ci/circleci: push-chart (pending)`, `circleci pipeline 12 (setup finished, the
continuation's workflows not created yet)`), so a wait that outlives its caller's patience has
named its cause. With `--progress` every poll says it.

A read GitHub or CircleCI does not answer is not an outcome: every poll reads the same state again, so
a reset connection, an EOF, a try that takes longer than 60 s or a 5xx is sent again after 2 s, the
pause doubling up to 60 s, for up to fifteen tries in a row (about ten minutes of pauses), never past
the timeout. Each retried failure is a warning with its time:

```
2026-09-23T10:11:12Z GET https://api.github.com/repos/giantswarm/devctl/pulls/2360: 500 Internal Server Error; retried in 2s (try 2 of 15)
```

A read that fails all fifteen tries says nothing about the head either: the poll is unanswered, a
warning names it (`poll 3 went unanswered: Get "https://api.github.com/repos/giantswarm/devctl/pulls/2360":
failed 15 times in a row: 500 Internal Server Error`) and the wait polls again at the next interval. An
outage shorter than the timeout ends in the head's verdict; one that outlasts it is exit 2, the reason
adding that the head is unknown, not failed, and the last unanswered read. A 4xx is an answer and is never
retried, nor is a certificate the client refuses or a host that does not exist; only reads (GET, HEAD) are
retried.

GitHub's REST check-runs and combined-status endpoints can fail for hours while GraphQL answers. When
the read of the head's check runs or statuses fails all fifteen tries, the wait reads them from the head
commit's `statusCheckRollup` through GraphQL instead, warns once (`the REST check endpoints went
unanswered (…): the head's checks are read through GraphQL (statusCheckRollup)`), sets `checksVia` to
`graphql` and keeps reading GraphQL for the rest of the wait, going back to REST if a GraphQL read fails.
Only the checks fall back: the pull request, the Actions runs and, in `pr merge`, the merge itself still
need REST. With both paths failing the poll is unanswered, as above.

The wait reads nothing from stdin and ignores SIGHUP: a wait whose terminal goes away (a closed terminal
or SSH session, a harness that moves it to the background) keeps waiting and ends with the verdict.

A spent budget is not an outcome either. The GitHub budget belongs to the person, so other tools and
waits of the same person can spend it while a wait runs; a read GitHub refuses with `403` or `429` and
`X-RateLimit-Remaining: 0` is sent again a second after its `X-RateLimit-Reset`, one answered with
`Retry-After` (GitHub's secondary limit, CircleCI's `429`) that much later, a secondary limit that names
no time after a minute, and a `429` without either on the backoff. The warning names the limit and the
time it resets:

```
2026-09-23T16:48:03Z GET https://api.github.com/repos/giantswarm/devctl/pulls/2360: 403 Forbidden: the core rate limit (5000) is spent until 2026-09-23T17:50:00Z; retried in 1h1m58s (try 2 of 15)
```

An answer that spends the last request does not stop the reads after it: the client sends them and
GitHub's answer decides. A reset after the wait's deadline ends the wait at once with exit 2, the reason
naming the reset and the deadline (`… 403 Forbidden: the core rate limit (5000) is spent until
2026-09-23T17:50:00Z, after the wait's deadline at 2026-09-23T17:20:00Z; run the wait again after the
reset`). Any other `403` is an answer: a permission the token lacks.

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
| `command`, `schemaVersion`, `exitCode`, `verdict`, `reason`, `warnings`, `startedAt`, `finishedAt` | The envelope every agent-facing command prints. `verdict` is `green`, `red`, `timeout`, `not_applicable`, `required_missing`, `approval_required`, `no_checks`, `auth_required` or `usage`; `reason` is one sentence for anything but green; `warnings` carries the CircleCI token's expiry notice, a head change, a CircleCI project missing or never built, each retried read and each unanswered poll (Polling). |
| `repository`, `number` | The pull request as given. |
| `identity` | Who read GitHub: `app`, the devctl App login (giantswarm), or `gh`, your own `gh` login (every other owner); empty when the run ended before choosing. |
| `headSha`, `baseRef` | The head commit judged and the base branch whose protection was read. |
| `checks[]` | The head's check runs, every run of a name that counts, and its statuses, the latest per context, sorted by name and id. A check run carries its `id` and, when its job belongs to a GitHub Actions run of the head, its `workflow`. `source` is `check_run` or `status`; `status` is `queued`, `in_progress` or `completed` for a check run and `pending` or `completed` for a status; `conclusion` is the check run's conclusion or the status's state, empty while unfinished; `required` says whether the base requires this context. |
| `checksVia` | `graphql` when the last poll read `checks[]` through GraphQL's `statusCheckRollup` because REST's check endpoints went unanswered (Polling); absent when REST answered. |
| `circleci` | Present only when CircleCI was consulted: the newest pipeline of the head revision and its workflows, the newest run per name, sorted by name. |
| `actions[]` | The head's GitHub Actions runs, the latest per workflow name, sorted by name. `jobsCompleted` is true on a run that still reads `in_progress` while all its jobs completed five minutes ago or longer (rule 3). |
| `failedJobs[]` | Present at exit 1 with `--failed-log`: each failed job with `name` (an Actions check run's name, with `[<workflow>, check run <id>]` when several runs share it; `<workflow>/<job>` on CircleCI), `source` (`actions` or `circleci`), `url`, and `logTail`, the last `--failed-log-lines` lines of its log, led by the lines from its first failure when that lies before them, or `logError` when the log could not be read. A check run of another app, a commit status and a CircleCI workflow without a failed job are named in `reason` only. |
| `blocking[]` | Present on a green head that GitHub's merge box still holds: each pending check the base does not require, which the wait did not wait for (`check Heimdall - PR Gatekeeper (in_progress, not required, not waited for)`, `status <context> (pending, not required, not waited for)`), and, while the pull request's `mergeable_state` is `blocked`, the review the base requires (`review: an approving review the base requires is missing`, `review: a reviewer requested changes`), read from GitHub's `reviewDecision`. Each is a warning too. |
| `unfinished[]` | Present at exit 2 and 4: what the head was still waiting for, one line each (`check go-test (in_progress)`, `circleci workflow build (running)`, `actions run CI (awaiting approval)`, `required context lint (absent)`, `actions runs of .github/workflows/ci.yaml (none registered)`). |

## Exit codes

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `green` | Every rule above holds. |
| 1 | `red` | Something failed; `reason` names every failed check, status, run and workflow. |
| 2 | `timeout` | The timeout passed before an outcome, or a spent rate limit resets only after it; `unfinished` names what was still open, a required context still absent among it. |
| 3 | `not_applicable` | Draft, closed, merged, conflicting or behind a strict base; `reason` says which. |
| 4 | `required_missing` | Every check, run and workflow of the head has finished, the pull request's last update is more than a minute old (registration grace), and a required status context never reported; `reason` names it. Known at the poll that saw it, before the timeout; with anything still pending, another app's pending check included, the outcome is 2, not 4. |
| 4 | `approval_required` | Nothing is red and the head waits only for GitHub Actions runs awaiting a repository member's approval (and the required contexts they may report); `reason` names each run with its URL. Known at the poll that saw it; approve the runs and wait again. |
| 4 | `no_checks` | The head has no check run, status or Actions run two minutes after the pull request's last update, and the repository has workflows that run on `pull_request`: GitHub did not deliver the event. `reason` names the workflows; close and reopen the pull request to trigger them, and wait again. |
| 7 | `usage` | Wrong arguments or flags, a newer devctl released (the reason names `devctl version update`), or a tooling failure: GitHub or CircleCI answered with an error other than a 5xx. A read that fails every try is no tooling failure: the wait polls again (Polling). |
| 8 | `auth_required` | No usable token; `reason` names the `devctl auth login` to run. |

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `DEVCTL_GITHUB_API_URL` | `https://api.github.com` | The GitHub REST API. |
| `DEVCTL_CIRCLECI_API_URL` | `https://circleci.com/api/v2` | The CircleCI API v2. |
| `DEVCTL_KEYRING_FILE` | unset | A JSON file that holds the records in place of the OS keychain; read from the environment when a command starts, created with mode 0600 on first write. Unset or empty keeps the keychain (tests, machines without a keychain daemon) |
| `DEVCTL_TIME_SCALE` | `1` | Multiplies every poll interval and the timeout (tests). |
