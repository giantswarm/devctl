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
devctl pr rerun giantswarm/devctl 2277 && devctl pr wait giantswarm/devctl 2277
devctl release rerun giantswarm/devctl v8.123.0 && devctl release wait giantswarm/devctl v8.123.0
```

## A workflow wedged in running

A tag workflow whose jobs hang on CircleCI's side reads `running` for hours, and a cancel from the UI
may not even take. `release rerun` treats a workflow running (or failing) longer than
`--wedged-after` (default **2h**, far beyond any tag build; `0` turns it off) as wedged:

1. it is canceled (`POST /api/v2/workflow/{id}/cancel`) with the same CircleCI login as the rerun;
2. it is read until it no longer runs, at most two minutes (`ci.CancelTimeout`);
3. it is rerun from failed, or from start (`from_failed: false`) where CircleCI offers no rerun from
   failed for it. The workflow's `canceled` is `true`, `rerunFrom` says which, and a warning names its
   age and the bound.

Where CircleCI does not honour the cancel within those two minutes, the tag gets a fresh pipeline
through the API instead, as for a pipeline without a workflow below: `trigger`, once per tag. A
workflow on hold waits for a person and is never canceled. A running workflow younger than the bound
is refused as before (exit 5), the reason naming its age and the bound. `pr rerun` cancels nothing.

```nohighlight
devctl release rerun giantswarm/kagent-upstream v1.17.0 && devctl release wait giantswarm/kagent-upstream v1.17.0
```

## A pipeline without a workflow

A pipeline that never got a workflow has nothing to rerun: its setup workflow finished and the
continuation was never created, or the pipeline stays `pending` without a workflow (a CircleCI incident
that drops the continuation, as seen on tag pipelines whose setup job ran and nothing followed). The
way out is the way a repository admin takes by hand in the repository's webhook settings: the push
webhook delivery that created the pipeline is sent again, and CircleCI creates a new pipeline for the
head, the one `devctl pr wait` and `devctl release wait` read.

Both commands do that once the pipeline is **five minutes** old (`rerun.StalledAfter`; younger, CircleCI
may still create the workflow: exit 5 naming the age, run again later), through GitHub's webhook API:
the repository's hooks (`GET /repos/{o}/{r}/hooks`, the active one posting to `circleci.com`), the
hook's newest 100 deliveries (`GET …/hooks/{id}/deliveries`), the payload of each `push` delivery
(`GET …/deliveries/{id}`) until the head's push is found -- for a tag the delivery whose `ref` is
`refs/tags/<tag>`, for a pull request the one whose `after` is the head revision -- and its redelivery
(`POST …/deliveries/{id}/attempts`). The outcome is one line in `warnings` and the `redelivery` field.

Once per head: a delivery sent again before (a newer entry with its `guid` and `redelivery: true`) is
not sent a second time; exit 5 names the earlier redelivery, and what remains is CircleCI's status or an
empty commit. A head in a fork was pushed to the fork, whose deliveries the repository does not have:
exit 3. A pending pipeline whose setup workflow *failed* is rerun from failed like any other; one whose
setup workflow still runs is waited for (exit 5).

`release rerun` goes one step further where the push cannot be sent again -- it was sent again before,
or the repository has no CircleCI webhook or no longer lists the tag's push delivery, as for a tag whose
pipeline errored before any workflow: it triggers a fresh pipeline for the tag through CircleCI's API
(`POST /api/v2/project/gh/{o}/{r}/pipeline` with `{"tag": …}`), the one `devctl release wait` reads.
The outcome is one line in `warnings` and the `trigger` field. Once per tag: a pipeline without a
workflow that the API triggered itself (`trigger.type` `api` or `explicit`) gets no second one (exit 5).

## Credentials

A rerun is a write. It takes the CircleCI login of `devctl auth login` (the keychain, see
[auth.md](auth.md)) granted **Write** access on CircleCI's consent page; no other token is read. A
login that granted Read access only is refused by CircleCI with 403, which is exit 8 naming
`devctl auth login --circleci-only`. `pr rerun` reads the pull request on GitHub with the identity
`pr wait` uses: the devctl App login for giantswarm, your own `gh` login for every other owner.
`release rerun` reads GitHub only to redeliver a push webhook, with the same identity.

The redelivery needs webhook access: for the App, the repository permission **Webhooks: read and
write** of the `giantswarm-devctl` App (granted by an owner of the App under its Permissions & events and
approved on the organization's installation; every devctl login carries it from its next token refresh);
for your own `gh` login, a repository admin's login with the `repo` or `admin:repo_hook` scope. GitHub's
403 is exit 8 naming the permission the identity lacks, never a silent failure; until it is granted, a
repository admin redelivers the delivery under the repository's Settings → Webhooks → Recent Deliveries.

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
| `workflows[]` | `{name, id, status, url, failedJobs[], outcome, rerunId, rerunUrl, canceled, rerunFrom}`; `canceled` and `rerunFrom` (`failed` or `start`) only for a wedged workflow (`release rerun`) |
| `redelivery` | `{hookId, hookUrl, deliveryId, guid, deliveredAt, ref, after, outcome, redeliveredAt}`: the push delivery sent again for a pipeline without a workflow; absent otherwise |
| `trigger` | `{outcome, pipeline{id, number, url}}`: the fresh tag pipeline triggered through CircleCI's API (`release rerun`); absent otherwise |
| `identity` | `app` or `gh`, who read GitHub (`pr rerun`; `release rerun` when it redelivered) |

`outcome` is `rerun` (the rerun started; `rerunId` and `rerunUrl` are the new workflow),
`running` (not finished, not rerun), `no_failed_job` (canceled without a failed job), `nothing_to_rerun` (did not fail) or `refused` (CircleCI refused the
rerun; the reason says why). A workflow still running beside one that was rerun is named in
`warnings`. `redelivery.outcome` is `redelivered` (sent again; the warning says so),
`already_redelivered` (sent again before, at `redeliveredAt`; not sent a second time) or `refused`
(GitHub refused it; the reason names the permission). `trigger.outcome` is `triggered` (the fresh
pipeline is `trigger.pipeline`; the warning says so), `already_triggered` (the pipeline is itself one the
API triggered; no second one) or `refused` (CircleCI refused it; 403 is exit 8).

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
| 0 | `green` | at least one workflow is rerun from failed, a wedged one canceled and rerun, the push webhook delivery was sent again, or a fresh tag pipeline was triggered (`release rerun`) |
| 3 | `not_applicable` | no pipeline for the head or tag, no workflow with a failed job, or no push delivery to send again for a pull request (no CircleCI webhook, a fork's head): nothing to rerun |
| 5 | `refused` | nothing finished failed and a workflow is still running (for `release rerun`: within `--wedged-after`); a pipeline without a workflow younger than five minutes; a pull request's delivery sent again before; a tag pipeline the API triggered itself |
| 7 | `usage` | wrong usage or a tooling failure |
| 8 | `auth_required` | no CircleCI login, or one without Write access (`devctl auth login --circleci-only`); a GitHub identity without webhook access (the App's repository permission Webhooks: read and write) |
