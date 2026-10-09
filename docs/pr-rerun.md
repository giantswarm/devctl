# Rerunning failed CircleCI workflows: `devctl pr rerun` and `devctl release rerun`

```nohighlight
devctl pr rerun <owner/repo> <number>
devctl release rerun <owner/repo> <tag>
```

A CircleCI job that failed on a transient cause (a race between two pushes to an app catalog, a
flaky download) is fixed by rerunning its workflow from failed: the failed jobs and the jobs that
depend on them run again, the passed ones are kept. These commands do that from the command line
with the CircleCI login devctl already holds, instead of an empty commit that rebuilds the whole
pipeline or a new release candidate for a failed tag pipeline.

- `pr rerun` takes the pipeline CircleCI built for the pull request's head revision on the head's
  branch (`pull/<number>` for a head in a fork): the pipeline `devctl pr wait` reads.
- `release rerun` takes the newest pipeline of the tag; a version given as `X.Y.Z` or `vX.Y.Z` finds
  the tag in either spelling. GitHub is not read.

Of each workflow name the newest run counts, since a rerun is a new workflow of the same name in the
same pipeline. A finished workflow that failed (`failed`, `error`) is rerun
(`POST /api/v2/workflow/{id}/rerun` with `from_failed: true`) whatever its jobs read, since CircleCI's
authenticated job listing can lag behind the workflow; a canceled one is rerun when a job of it
failed. A workflow that is still running,
on hold or failing (a job failed while others still run) is not: CircleCI reruns a finished
workflow only. The rerun is started, not waited for; wait for it with the wait you would use anyway:

```nohighlight
devctl pr rerun giantswarm/devctl 2277 && /home/teemow/.go/bin/beekeeper gate -- devctl pr wait giantswarm/devctl 2277
devctl release rerun giantswarm/devctl v8.123.0 && /home/teemow/.go/bin/beekeeper gate -- devctl release wait giantswarm/devctl v8.123.0
```

## Credentials

A rerun is a write. It takes the CircleCI login of `devctl auth login` (the keychain, see
[auth.md](auth.md)) granted **Write** access on CircleCI's consent page; no other token is read. A
login that granted Read access only is refused by CircleCI with 403, which is exit 8 naming
`devctl auth login --circleci-only`. `pr rerun` reads the pull request on GitHub with the identity
`pr wait` uses: the devctl App login for giantswarm, your own `gh` login for every other owner.

## The document

One JSON document on stdout, the envelope of every agent-facing command (`command`,
`schemaVersion`, `exitCode`, `verdict`, `reason`, `warnings`, `startedAt`, `finishedAt`) and:

| Field | |
|---|---|
| `repository` | `owner/repo` |
| `number` | the pull request (`pr rerun`) |
| `tag` | the tag as CircleCI built it (`release rerun`) |
| `headSha` | the revision the pipeline built |
| `pipeline` | `{id, number, url}`; absent when CircleCI has no pipeline for the head or tag |
| `workflows[]` | `{name, id, status, url, failedJobs[], outcome, rerunId, rerunUrl}` |
| `identity` | `app` or `gh`, who read GitHub (`pr rerun`) |

`outcome` is `rerun` (the rerun started; `rerunId` and `rerunUrl` are the new workflow),
`running` (not finished, not rerun), `no_failed_job` (canceled without a failed job), `nothing_to_rerun` (did not fail) or `refused` (CircleCI refused the
rerun; the reason says why). A workflow still running beside one that was rerun is named in
`warnings`.

```json
{
  "command": "release rerun",
  "schemaVersion": 1,
  "exitCode": 0,
  "verdict": "green",
  "reason": "",
  "warnings": [],
  "repository": "giantswarm/devctl",
  "tag": "v8.123.0",
  "headSha": "27b21bbfd09cf906d8ecce7d33ad37039f1d631b",
  "pipeline": {"id": "b311160a-…", "number": 3885, "url": "https://app.circleci.com/pipelines/github/giantswarm/devctl/3885"},
  "workflows": [
    {"name": "build", "id": "f1290c29-…", "status": "failed", "url": "…/workflows/f1290c29-…",
     "failedJobs": ["push-chart"], "outcome": "rerun", "rerunId": "4be1…", "rerunUrl": "…/workflows/4be1…"}
  ]
}
```

## Exit codes

| Code | Verdict | |
|---|---|---|
| 0 | `green` | at least one workflow is rerun from failed |
| 3 | `not_applicable` | no pipeline for the head or tag, or no workflow with a failed job: nothing to rerun |
| 5 | `refused` | nothing finished failed and a workflow is still running; wait and rerun if it fails |
| 7 | `usage` | wrong usage or a tooling failure |
| 8 | `auth_required` | no CircleCI login, or one without Write access (`devctl auth login --circleci-only`) |
