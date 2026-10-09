# CircleCI pipelines at the job level: `devctl ci jobs` and `devctl ci rerun`

```nohighlight
devctl ci jobs <owner/repo> <pipeline number|id>
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
not answer are warnings, not an outcome: the rest of the view is read.

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
| 3 | `not_applicable` | no such workflow, or one of another repository |
| 5 | `refused` | a rerun of the workflow's name within the hour, a workflow still running without `--cancel`, or a rerun CircleCI refused (the reason carries its message) |
| 7 | `usage` | wrong usage or a tooling failure |
| 8 | `auth_required` | no CircleCI login, or one without Write access (`devctl auth login --circleci-only`) |

`ci jobs` exits 0 (`listed`), 3 (no such pipeline of the repository), 7 or 8.
