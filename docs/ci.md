# CircleCI pipelines at the job level: `devctl ci jobs`, `devctl ci job` and `devctl ci rerun`

```nohighlight
devctl ci jobs <owner/repo> <pipeline number|id>
devctl ci job <owner/repo> <job number> [--step-output] [--lines <n>]
devctl ci job <job URL> [--step-output] [--lines <n>]
devctl ci rerun <owner/repo> <workflow id> [--from-failed] [--cancel]
```

`devctl pr wait`, `devctl release wait` and their reruns see a pipeline at the workflow level: a
workflow that reads `running` for hours looks the same whether a job is slow (a long build that is
still writing output) or stuck (no output for an hour, a job CircleCI stopped updating). These two
commands go one level down, with the CircleCI login devctl already holds (the keychain, see
[auth.md](auth.md)), so nobody needs a CircleCI token of their own or the CircleCI UI to tell the
two apart and to recover a stuck workflow. GitHub is not read: a pipeline or a workflow is named by
what CircleCI calls it, the number or id the UI and `devctl pr wait` show.

## `ci jobs`: where every job stands

The pipeline is read in full: every workflow run of it, reruns included (a rerun is a second run of
the same name in the same pipeline; the newest run of a name is marked `latest`), with its jobs,
each with its status, number, when it started and stopped and how long it ran or has been running.
A job that is running also carries the step it is in, the last one that started: when the step
started, when it last wrote a line of output (the step's output, read from CircleCI's v1.1 job
detail) and how long ago that was.

- A slow job: `step.outputAgeSeconds` is small, the step keeps writing.
- A stuck job: `step.outputAgeSeconds` is in the thousands, or `lastOutputAt` is absent and
  `runningSeconds` is large.
- A workflow `running` with every job `success` and no job running: CircleCI's workflow status lags
  behind its jobs; nothing is stuck, the wait is on CircleCI.

A login that granted Read access suffices. Jobs CircleCI does not list yet, steps or output it does
not answer are warnings, not an outcome: the rest of the view is read. Caller cancellation ends the
inspection with an error while preserving the view assembled so far.

```nohighlight
devctl ci jobs giantswarm/devctl 3885
```

```json
{
  "command": "ci jobs", "schemaVersion": 1, "exitCode": 0, "verdict": "listed", "reason": "", "warnings": [],
  "repository": "giantswarm/devctl",
  "pipeline": {"id": "b311160a-…", "number": 3885, "state": "created", "url": "https://app.circleci.com/pipelines/github/giantswarm/devctl/3885",
               "createdAt": "2026-10-09T17:00:00Z", "tag": "v8.123.0", "revision": "27b21bbf…"},
  "workflows": [
    {"name": "build", "id": "f1290c29-…", "status": "failed", "url": "…/workflows/f1290c29-…",
     "createdAt": "2026-10-09T17:01:00Z", "stoppedAt": "2026-10-09T17:10:00Z", "latest": false,
     "jobs": [{"name": "push-chart", "number": 7781, "type": "build", "status": "failed",
               "startedAt": "2026-10-09T17:02:00Z", "stoppedAt": "2026-10-09T17:10:00Z", "durationSeconds": 480}]},
    {"name": "build", "id": "4be1…", "status": "running", "url": "…/workflows/4be1…",
     "createdAt": "2026-10-09T17:20:00Z", "latest": true,
     "jobs": [
       {"name": "push-chart", "number": 7790, "type": "build", "status": "running",
        "startedAt": "2026-10-09T17:22:30Z", "durationSeconds": 2250,
        "step": {"name": "Push chart to the catalog", "startedAt": "2026-10-09T17:23:00Z", "runningSeconds": 2220,
                 "lastOutputAt": "2026-10-09T17:24:00Z", "outputAgeSeconds": 2160}}
     ]}
  ]
}
```

## `ci job`: steps and output of one job

One job is read by its number in the project, the number its page
(`https://circleci.com/gh/<owner>/<repo>/<number>`), a CircleCI commit status and `ci jobs` show: its
name, status, page and times, every step (a parallel step once per action) with its status and times,
and in `job.logTail` the last lines (`--lines`, 200 by default) of the output of its failed steps, led
by the lines from its first failure when that lies before them, without terminal control sequences. It
is the tail `devctl pr wait --failed-log` reads, for a job of any pipeline: a tag's, a branch's, one no
pull request waits on. The CircleCI login is used in devctl's process only, a step's output is read
from its signed URL without it, so a session reads a failed job without a token of its own, a browser
or the CircleCI UI. Output CircleCI does not answer is `job.logError`; the steps still stand.
Caller cancellation during a step or output read ends the inspection with an error and retains the
job data already assembled.

Use `--step-output` to include output from **every** step, successful ones included. Each action
gets `outputHead` and `outputTail`, the first and last `--lines` lines (200 each by default), with
terminal controls removed. The excerpts overlap for short output. A step whose output cannot be
read gets `outputError`; the other steps are still read. Empty output and steps without an output
URL have no excerpts. An absent excerpt does not prove a scan ran.

A nancy step can read `success` even when the scanner could not answer. Its output says
`Error: guide API request failed` or `NOT SCANNED`, while a completed scan prints its audited and
vulnerable dependency counts. Read that evidence without the UI:

```nohighlight
devctl ci job giantswarm/devctl 7781 --step-output
```

Look for `Check if dependencies have known security vulnerabilities` in `job.steps`: its status
stays as CircleCI reported it, and its excerpts distinguish the outage from a completed scan.
`job.logTail` continues to contain only failed steps' output.

The job can also be named by the URL a CircleCI commit status links, alone. A GitHub App pipeline's
status links `https://app.circleci.com/workflow/<id>/job/<id>`, which names neither the project nor the
number: the command reads the workflow for its project and finds the job's number among its jobs. A job
that has not started has no number yet and is not applicable (exit 3).

```nohighlight
devctl ci job giantswarm/agent-toolchains-lab 7 | jq -r .job.logTail
```

```json
{
  "command": "ci job", "schemaVersion": 1, "exitCode": 0, "verdict": "listed", "reason": "", "warnings": [],
  "repository": "giantswarm/agent-toolchains-lab",
  "job": {"number": 7, "name": "push-go", "status": "failed", "url": "https://circleci.com/gh/giantswarm/agent-toolchains-lab/7",
          "pipelineId": "137d83a2-…", "workflowId": "3f96b11b-…",
          "startedAt": "2026-10-22T23:25:44.983Z", "stoppedAt": "2026-10-22T23:25:47.057Z",
          "steps": [{"name": "Checkout code", "index": 0, "status": "success", "startedAt": "…", "endedAt": "…"},
                    {"name": "Build multi-arch image (pushed to all registries unless push=false)", "index": 0, "status": "failed", "startedAt": "…", "endedAt": "…"}],
          "logTail": "Building for platforms: linux/amd64\nNo valid registry/tag combinations found for push. Exiting.\n\nExited with code exit status 1\n"}
}
```

## `ci rerun`: one workflow, once an hour

One workflow is rerun: every job of it, or with `--from-failed` its failed jobs and the jobs that
depend on them, the passed ones kept (`POST /api/v2/workflow/{id}/rerun`). The workflow has to
belong to the repository; the rerun is started, not waited for (`devctl pr wait` and
`devctl release wait` wait for it).

**One rerun per hour.** When the newest run of the workflow's name in its pipeline is a rerun made
less than an hour ago, the call is refused (exit 5) naming that rerun and when the hour ends,
whichever run of the name was given and whoever started the rerun (the rule reads CircleCI, not a
file). An agent that reruns on every poll therefore cannot loop; `devctl ci jobs` shows where the
rerun stands meanwhile.

**A workflow still running is refused** (exit 5): `devctl ci jobs` tells a slow job from a stuck
one. `--cancel` cancels it first (`POST /api/v2/workflow/{id}/cancel`), waits up to two minutes for
CircleCI to read it canceled and reruns it: the recovery of a stuck workflow. A cancel that does not
settle in time is exit 2; the rerun is then one call away once the workflow reads canceled.

```nohighlight
devctl ci rerun giantswarm/devctl f1290c29-… --from-failed && devctl release wait giantswarm/devctl v8.123.0
devctl ci rerun giantswarm/devctl 4be1… --cancel
```

### Credentials

A rerun and a cancel are writes: they take the CircleCI login of `devctl auth login` granted
**Write** access on CircleCI's consent page; no other token is read. A login that granted Read
access only is refused by CircleCI with 403, which is exit 8 naming `devctl auth login --circleci-only`.

### The document

One JSON document on stdout, the envelope of every agent-facing command (`command`,
`schemaVersion`, `exitCode`, `verdict`, `reason`, `warnings`, `startedAt`, `finishedAt`) and:

| Field | |
|---|---|
| `repository` | `owner/repo` |
| `pipeline` | `{id, number, url}`; absent when CircleCI has no such workflow |
| `workflow` | `{name, id, status, url, createdAt, stoppedAt}`, the workflow as given; `status` is `canceled` after `--cancel` |
| `fromFailed` | whether the rerun is from failed |
| `canceled` | whether the workflow was canceled before the rerun |
| `outcome` | `rerun` (the rerun started) or `refused` (the reason says why) |
| `rerunId`, `rerunUrl` | the new workflow of a rerun |
| `lastRerun` | `{name, id, status, url, createdAt, stoppedAt}`, the rerun within the hour that refused this call |

A run of the name newer than the one given is named in `warnings`; the rerun is of the one given.

### Exit codes

| Code | Verdict | |
|---|---|---|
| 0 | `green` | the rerun started; `rerunId` and `rerunUrl` are the new workflow |
| 2 | `timeout` | `--cancel`: the workflow still reads running two minutes after the cancel |
| 2 | `transient` | a read exhausted its bounded retry window; the reason names the attempts, elapsed time and last API or network failure |
| 3 | `not_applicable` | no such workflow, or one of another repository |
| 5 | `refused` | a rerun of the workflow's name within the hour, a workflow still running without `--cancel`, or a rerun CircleCI refused (the reason carries its message) |
| 7 | `usage` | wrong usage or a tooling failure |
| 8 | `auth_required` | no CircleCI login, or one without Write access (`devctl auth login --circleci-only`) |
| 13 | `remote_error` | A CircleCI request failed. The command line was valid; the reason preserves the service error. Existing timeout and post-merge outcomes take precedence. |

`ci jobs` exits 0 (`listed`), 2 (`transient`), 3 (no such pipeline of the repository), 7, 8 or 13 (`remote_error`).

## Cancel workflows and obsolete queued pipelines

```sh
devctl ci cancel giantswarm/devctl <workflow-id>
devctl ci cancel giantswarm/devctl <pipeline-id> --pipeline
devctl ci cancel giantswarm/devctl --stale --dry-run
devctl ci cancel giantswarm/devctl --stale
```

The explicit forms cancel active workflows after checking repository ownership.
Pipeline mode visits every workflow page, including setup workflows. Finished
workflows are skipped. Cancellation requires a CircleCI login with Write access.

`--stale` visits every project pipeline page and reads current GitHub branch and
pull request state using the existing GitHub login. A different head revision or
a closed PR makes a branch pipeline obsolete. Fork pipelines named `pull/N` are
checked against that PR. Tags and the default branch are protected. A missing
branch without a known closed PR is retained; failed reads stop the command.
Only workflows whose jobs are all queued, blocked or not yet run are canceled:
started jobs, unknown job states, empty job listings and approval jobs are retained.
The jobs are read again immediately before cancellation. The APIs do not offer
an atomic check-and-cancel, so an executor can still start between that read and
the cancellation request.

The JSON envelope follows the other `ci` commands (schema version 1), with
`repository`, `dryRun`, `workflows` and `uncancelablePipelines`. Each workflow
includes `id`, `pipelineId`, `status` and `outcome`: `skipped`, `would_cancel`, or
`cancel_requested`. The latter records API acceptance, not terminal cancellation;
use `ci jobs` to inspect completion. Partial results remain in the document if a
later request fails. Exit codes follow the agent envelope: 0 success, 3 repository
mismatch, 5 refused write, 7 usage/tooling failure, 8 authentication required.

CircleCI API v2 exposes cancellation for workflows, not pipeline objects. A
pipeline with no workflows cannot be canceled through this API and its ID is
reported in `uncancelablePipelines`; the command does not claim to have canceled it.
