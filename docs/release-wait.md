# Waiting for a release: `devctl release wait`

```nohighlight
devctl release wait <owner/repo> (<vX.Y.Z | X.Y.Z> | --pr <number>) [--timeout 30m] [--catalog] [--progress]
```

Blocks until every image and chart of a release is pullable and the tag's pipeline is green, then prints
one JSON document and exits with a code that says what happened. It is the command an agent runs after a
merge instead of guessing: the tag and the GitHub Release exist about a minute after the merge, the
artifacts come from the CircleCI pipeline the **tag** triggers, minutes later, under names the
repository's CI decides. Nothing here is guessed from the repository name, `helm search` or the merge
commit's Actions run.

```nohighlight
devctl release wait giantswarm/devctl v8.9.0
devctl release wait giantswarm/devctl --pr 2289 --timeout 20m --progress
devctl release wait giantswarm/app-operator 7.5.4 --catalog
```

Tokens come from the keychain (`devctl auth login`, see [auth.md](auth.md)). GitHub is always
needed; CircleCI only when the tag carries a `.circleci/config.yml`. A missing token is exit 8 with
the login command in `reason`, before anything is waited for.

## What the command reads, and why

### The version

- `vX.Y.Z` or `X.Y.Z`: the tag is looked up under both spellings, the given one first, and the wait
  starts when it exists. A tag that never appears is a timeout (exit 2).
- `--pr <number>`: the pull request must be merged (otherwise exit 3). In a repository on the
  **auto-release** model the tag is the one on the merge commit; the command waits for it to appear,
  and the merge commit's auto-release run (the push run of the `auto_release` workflow) says when it
  will not: a run that finished without a tag decided that the commits since the last release warrant
  no bump, which is exit 3 with the verdict `no_release` at once instead of a timeout; a run that
  failed before it tagged is exit 1; a run cancelled before it tagged was superseded by a newer push
  to the branch, whose tag carries the merge, exit 3 naming it. A repository on the **legacy** model
  (`create_release` workflows, a release pull request) does not tag the merge commit of a feature
  pull request, nor does one without a release workflow that no entry declares a model for: `--pr` is
  exit 3, `no_release`, with one sentence. [`devctl pr merge`](pr-merge.md) runs this wait after its
  merge, and reads `no_release` as a merge that is done.

### The release model

`releaseModel` is `auto-release` or `legacy`. Two sources say which:

1. The team-file entry of the repository in `repositories/<team>.yaml` of giantswarm/github:
   `gen.ci.releaseWorkflow` when set, else `auto-release` when `gen.ci.generate` is true and
   `legacy` otherwise. An entry without a `gen` block declares nothing.
2. The workflow files at the tag under `.github/workflows`: an `auto_release` / `auto-release`
   workflow means auto-release, `create_release` / `create-release` workflows mean legacy.

A source that says nothing leaves the decision to the other; neither saying anything is exit 7. When
the two disagree, the workflows at the tag decide -- they made the tag -- and `warnings` names the
mismatch with its remedy: `declaration says legacy, repository runs auto-release: the team-file entry
resolves gen.ci.releaseWorkflow to legacy while the workflows at <sha> are the auto-release ones
(zz_generated.auto_release.yaml); the repository's workflows decide, align the team-file entry …`.
That is a repository whose generated pipeline and auto-release workflow merged while its entry still
resolves `legacy` (the declaration lands in giantswarm/github later, by a person), or the reverse,
an entry switched before align-files rendered the workflow. The CI model below is stricter: an entry
that declares the generated pipeline explicitly has to match the files, because it names the
artifacts.

### The CI model and the artifact names

`ciModel` is `generated`, `hand-written` or `none`, from the files at the tag: no
`.circleci/config.yml` is `none`; a `.circleci/workflows.yml` beside it is the signature of
`devctl gen circleci` and means `generated`; a `config.yml` alone is `hand-written`. The entry's
`gen.ci.generate` is cross-checked against it (`true` without the generated files, or an explicit
`false` with them, is exit 7).

**Generated CI**: the artifacts are what devctl's generator emits for the entry, derived the way the
generator derives them (an entry without a `gen.ci` block declares no override, so the generator's
defaults name them; a repository no team file declares is exit 7, there being no entry to name them):

| Artifact | When | Name | Registry |
|---|---|---|---|
| image | a `Dockerfile` at the repository root of the tag, or `gen.ci.image.dockerfile` set | `gen.ci.image.name`, else `giantswarm/<repo>` | private with `gen.ci.image.privateOnly`, or for a private repository without `gen.ci.forcePublic`; public otherwise |
| chart | `gen.flavours` contains `app` and the repository is not a template | `gen.ci.chartName`, else `<repo>` | private for a private repository without `gen.ci.forcePublic`; public otherwise |

The chart's catalog is `gen.ci.appCatalog`, default `giantswarm-catalog`.

The table names what the generator renders, not what a repository adds: jobs in `.circleci/custom.yml`,
which the setup workflow merges into the build workflow, push and sign artifacts of their own (vm-manager's
guest image, muster's CRD chart, backstage's control-plane catalog entry). They are not probed by name;
the release waits for them through the tag pipeline being green (below).

**Hand-written CI**: the artifacts are the push jobs the tag pipeline runs. The command reads the
`.circleci/config.yml` (and `workflows.yml`, `custom.yml` when present) at the tag, collects every
`<orb>/push-to-registries`, `push-to-registries-multiarch`, `push-to-docker` and
`push-to-app-catalog` job of every workflow with its parameters (`name`, `image`, `chart`,
`app_catalog`, `push`, `push_to_oci_registry`, `registries-data`, `force-public`), and keeps the
ones whose name CircleCI lists among the jobs of the tag pipeline's workflows. An image job without
`image` is the orb's default, `<owner>/<repo>`; `push: false` and a chart job that pushes to
neither the catalog nor the registry are build-only and skipped; `registries-data` that names only
the private registry makes the image private. Because the pipeline's jobs are the source, the
command waits for the pipeline to exist before it knows the artifacts.

When a `Dockerfile` exists at the tag and no push job of the pipeline names an image, the sources
disagree: exit 7 with the jobs seen. The repository name is never used as a fallback.

**Neither image nor chart** (a CLI that ships binaries as release assets, a repository without
CircleCI): the release is available when the GitHub Release of the tag is published (not a draft) and
every workflow of the tag finished green. `artifacts` then lists the release assets with the digests
GitHub reports.

### Availability

A release is available when every artifact resolves to a digest **and** the tag's CI is green (the next
section). The artifacts alone are not the release: a repository's own tag jobs push more than the sources
name, and a job signs what it pushed after the digest resolves.

An artifact is available when its manifest resolves to a digest:

- images at `<registry>/<name>:<X.Y.Z>`, charts at `<registry>/charts/<owner>/<chart>:<X.Y.Z>`
  (the version without its `v`);
- the public registry (`gsoci.azurecr.io`) is probed **anonymously**: it is public, and a stale
  `docker login` in `~/.docker/config.json` would otherwise make the probe send an expired token and
  read the registry's `UNAUTHORIZED` as "not yet available" for as long as the timeout;
- the private registry (`gsociprivate.azurecr.io`) is probed with the docker keychain, the
  credentials `docker login` stored;
- `MANIFEST_UNKNOWN` (and `NAME_UNKNOWN`, a first release into a repository nothing was pushed to
  yet) means not yet; **any other answer** is the probe's failure, not a slow pipeline, and ends the
  wait as exit 7 at once: a 401 from the private registry names `docker login`, a 401 from the
  public registry says the artifact is not public. A connection failure is retried twice before it
  counts as such.

`--catalog` additionally waits until `https://giantswarm.github.io/<catalog>/index.yaml` lists every
chart at the version; the index is fetched anew every time, so no cached copy answers.

### The tag's CI

Every poll reads the tag pipeline: the newest CircleCI pipeline whose `vcs.tag` is the tag, its
workflows reduced to the **newest run per workflow name** (a rerun, from failed or in full, is a
second workflow of the same name in the same pipeline, and the one it replaces keeps its failed
status for ever). A workflow in `failed`, `error`, `failing`, `canceled` or `unauthorized` ends the
wait with exit 1 and `pipeline.failedJobs` (`workflow/job`). A workflow CircleCI knows by id but
answers 404 on the jobs of -- the setup workflow for a short while after the pipeline is created --
is not finished: the poll goes on and `pipeline.unfinished` says `setup (running, jobs not visible
yet)`; the same 404 on a finished workflow is a tooling failure (exit 7). A repository without CircleCI is judged
by the GitHub Actions runs on the tag's commit whose branch is the tag: a `failure`, `cancelled`,
`timed_out` or `startup_failure` conclusion is exit 1.

The wait requires the tag pipeline to be green: every workflow (newest run per name) finished and at
least one succeeded, `not_run` counting as neither. Artifacts that resolve while a workflow still runs, a
repository-owned job or the Aliyun mirror (`sync-china-registry`, which typically ends within a minute of
the chart push), leave the wait polling, with `every artifact is available; pipeline N unfinished: build
(running)` on `--progress`. The pipeline decides the verdict whatever the order: a job that fails after
the artifacts resolved is exit 1 with that job in `failedJobs`, and a pipeline that does not finish in
time is exit 2 with `every artifact of <tag> is available, the tag pipeline did not finish within <timeout>;
pipeline N unfinished: …`. A document with exit 0 never lists an unfinished workflow.

### Polling

Requests to GitHub are conditional (`If-None-Match` with the last `ETag`), so an unchanged resource
costs a 304 that does not count against the rate limit. The interval between polls follows the
`X-RateLimit-Remaining` / `X-RateLimit-Reset` headers of the responses, between 15 and 60 seconds;
the rate_limit endpoint is never asked. `DEVCTL_TIME_SCALE` multiplies every sleep and the timeout
(the end-to-end tests run at 0.001). A GitHub or CircleCI read that fails in transit (a reset
connection, an EOF, a try over 60 s) or with a 5xx is sent again, up to eight tries in a row with a
pause from 2 s doubling to 60 s, each retried failure a warning with its time, as in
[`pr wait`](pr-wait.md#polling); a read that fails all eight is exit 7 naming the request and the count.

## The document

One JSON document on stdout at the end, nothing else; `--progress` writes one line per step to
stderr.

```json
{
  "command": "release wait",
  "schemaVersion": 1,
  "exitCode": 0,
  "verdict": "available",
  "reason": "",
  "warnings": [],
  "startedAt": "2026-09-21T10:00:00Z",
  "finishedAt": "2026-09-21T10:06:12Z",
  "repository": "giantswarm/kserve",
  "tag": "v1.2.3",
  "sha": "0123456789abcdef0123456789abcdef01234567",
  "releaseModel": "auto-release",
  "ciModel": "generated",
  "artifacts": [
    {"kind": "image", "reference": "gsoci.azurecr.io/giantswarm/kserve-controller:1.2.3", "digest": "sha256:…", "state": "available"},
    {"kind": "chart", "reference": "gsoci.azurecr.io/charts/giantswarm/kserve:1.2.3", "digest": "sha256:…", "state": "available"}
  ],
  "pipeline": {
    "id": "8c4b…",
    "number": 1234,
    "url": "https://app.circleci.com/pipelines/github/giantswarm/kserve/1234",
    "workflows": [{"name": "build", "status": "success"}, {"name": "setup", "status": "success"}],
    "failedJobs": [],
    "unfinished": []
  },
  "actions": []
}
```

| Field | Meaning |
|---|---|
| `command`, `schemaVersion`, `exitCode`, `verdict`, `reason`, `warnings`, `startedAt`, `finishedAt` | The envelope every agent-facing command prints. `verdict` is `available`, `ci_failed`, `timeout`, `not_applicable`, `usage` or `auth_required`. `warnings` carries the CircleCI token's seven-day expiry notice and a team-file declaration that disagrees with the tag's workflows about the release model. |
| `repository`, `tag`, `sha` | What was waited for. `tag` is empty when it never appeared; `sha` is the tag's commit (with `--pr` the merge commit). |
| `releaseModel` | `auto-release` or `legacy`. |
| `ciModel` | `generated`, `hand-written` or `none`. |
| `artifacts[]` | `kind` (`image`, `chart`, `release-asset`), `reference` (the pullable reference, or the asset's download URL), `digest` (empty while missing), `state` (`available`, `missing`). Empty until the artifacts are known (hand-written CI before its pipeline exists). |
| `pipeline` | The tag pipeline on CircleCI: `id`, `number`, `url`, `workflows[{name, status}]` (newest run per name), `failedJobs[]`, `unfinished[]` (the workflows not finished, `name (status)`, with `jobs not visible yet` when CircleCI does not list them yet; what a timeout was waiting for, empty at exit 0). `null` for a repository without CircleCI, or while the pipeline does not exist. |
| `actions[]` | The Actions runs the tag triggered, for a repository without CircleCI: `name`, `runId`, `status`, `conclusion`, `url`. |

## Exit codes

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `available` | Every artifact resolves to a digest and every workflow of the tag pipeline finished green (and, with `--catalog`, the index lists every chart); or the release of a repository without image and chart is published with its workflows green. |
| 1 | `ci_failed` | A workflow of the tag pipeline, or an Actions run of the tag, failed or was cancelled; with `--pr`, the merge commit's auto-release run failed before it tagged. `reason` and `pipeline.failedJobs` name the jobs. The release is incomplete until a rerun of the failed workflow succeeds or a fix lands as the next tag; artifacts that did resolve are listed `available`. |
| 2 | `timeout` | The deadline passed. `reason` names what is missing: the tag, the pipeline, the artifacts by reference, and the pipeline's unfinished workflows (also when every artifact is already available). |
| 3 | `not_applicable` | The pull request is not merged, or its auto-release run was cancelled before it tagged (superseded by a newer push), so `--pr` cannot resolve a version. |
| 3 | `no_release` | No release follows the pull request's merge: the repository does not tag merge commits (the legacy release model, or no release workflow at all), or the merge commit's auto-release run finished without a tag. |
| 7 | `usage` | A bad argument or flag; a newer devctl released (the reason names `devctl version update`); no source says how the repository releases; the sources disagree about the CI model or the artifacts; a registry answer that is neither a digest nor "manifest unknown"; a tooling error, a GitHub or CircleCI read that failed eight tries in a row among them. |
| 8 | `auth_required` | No usable token in the keychain; `reason` names the `devctl auth login` invocation. |

## Environment

The production endpoints are the defaults; the variables exist for another site and for the
end-to-end tests.

| Variable | Default | Effect |
|---|---|---|
| `DEVCTL_GITHUB_API_URL` | `https://api.github.com` | The GitHub REST API. |
| `DEVCTL_CIRCLECI_API_URL` | `https://circleci.com/api/v2` | The CircleCI API v2. |
| `DEVCTL_REGISTRY_PUBLIC` | `gsoci.azurecr.io` | The public registry, probed anonymously. |
| `DEVCTL_REGISTRY_PRIVATE` | `gsociprivate.azurecr.io` | The private registry, probed with the docker keychain. |
| `DEVCTL_REGISTRY_INSECURE` | unset | `1` talks plain HTTP to the registries (tests). |
| `DEVCTL_CATALOG_URL` | `https://giantswarm.github.io` | The host of the catalog indexes (`<host>/<catalog>/index.yaml`). |
| `DEVCTL_KEYRING_FILE` | unset | A 0600 JSON file in place of the OS keychain (tests). |
| `DEVCTL_TIME_SCALE` | `1` | Multiplies every sleep and the timeout. |

## The requests, for a mock

The end-to-end harness (`e2e/README.md`) scripts these endpoints; a scenario of this command needs
the ones its path takes.

GitHub: `GET /repos/{o}/{r}/pulls/{n}` (with `--pr`), `GET /repos/{o}/{r}/tags` (with `--pr`, the
tag on the merge commit), `GET /repos/{o}/{r}/git/ref/tags/{tag}` (and `GET
/repos/{o}/{r}/git/tags/{sha}` for an annotated tag), `GET /repos/{o}/{r}` (private or not), `GET
/repos/{o}/{r}/contents/` with `?ref=` (the root listing: the Dockerfile), `GET
/repos/{o}/{r}/contents/.github/workflows`, `GET /repos/{o}/{r}/contents/.circleci`, `GET
/repos/{o}/{r}/contents/.circleci/config.yml` (hand-written CI; `workflows.yml`, `custom.yml` when
listed), `GET /repos/giantswarm/github/contents/repositories` and `GET
/repos/giantswarm/github/contents/repositories/{team}.yaml` (the team-file entry, until the one that
declares the repository), `GET /repos/{o}/{r}/releases/tags/{tag}` and `GET
/repos/{o}/{r}/actions/runs?head_sha={sha}` (release assets, no CircleCI; with `--pr`, the merge
commit's auto-release run while no tag is on it).

CircleCI: `GET /api/v2/project/gh/{o}/{r}/pipeline` (the tag pipeline by `vcs.tag`, newest pages),
`GET /api/v2/pipeline/{id}/workflow`, `GET /api/v2/workflow/{id}/job`.

Registries: `GET /v2/` then `HEAD /v2/{name}/manifests/{X.Y.Z}`, images under `giantswarm/…`, charts
under `charts/giantswarm/…`.
