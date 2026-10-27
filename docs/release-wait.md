# Waiting for a release: `devctl release wait`

```nohighlight
devctl release wait <owner/repo> (<vX.Y.Z | X.Y.Z | image/vX.Y.Z> | --pr <number>) [--timeout 30m] [--catalog] [--image <owner/name>]... [--chart <path>]... [--progress] [--failed-log [--failed-log-lines 50]]
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
devctl release wait giantswarm/dex v2.43.3 --image giantswarm/dex
```

Tokens come from the keychain (`devctl auth login`, see [auth.md](auth.md)). GitHub is always
needed; CircleCI only when the tag carries a `.circleci/config.yml`. A missing token is exit 8 with
the login command in `reason`, before anything is waited for.

## What the command reads, and why

### The version

- `vX.Y.Z` or `X.Y.Z`: the tag is looked up under both spellings, the given one first, and the wait
  starts when it exists. A tag that never appears is a timeout (exit 2).
- `<image>/vX.Y.Z`: a release of one image of a `multi-image` repository (one image per
  `toolchains/<image>/` directory, each its own release line), under that one spelling. Its artifact is
  the image `<owner>/<repo>/<image>:<X.Y.Z>`. With `--pr`, a merge commit that carries one image's
  tag is followed like a `vX.Y.Z` tag. One that carries several (a change to the build files the images
  share releases every image) ends with exit 11, naming each tag to wait for.
- `--pr <number>`: the pull request must be merged (otherwise exit 3). In a repository on the
  **auto-release** model the tag is the one on the merge commit, on whatever branch the pull request
  was merged into (a release branch's tag of an older line included); the command waits for it to
  appear, and the merge commit's auto-release run (the push run of the `auto_release` workflow) says
  when it will not. A run that finished without a tag decided that the commits since the last release
  warranted no bump: the release that carries the merge is then the first tag on the pull request's
  base branch after the merge commit, and without one it is exit 3 with the verdict `no_release` at
  once instead of a timeout, the reason naming the base branch; a run that
  failed before it tagged is exit 1; a run cancelled before it tagged was superseded by a newer push
  to the branch, whose tag carries the merge, exit 3 naming it. A repository on the **legacy** model
  (`create_release` workflows, a release pull request) does not tag the merge commit of a feature
  pull request, nor does one without a release workflow that no entry declares a model for: `--pr` is
  exit 3, `no_release`, with one sentence. [`devctl pr merge`](pr-merge.md) runs this wait after its
  merge, and reads `no_release` as a merge that is done. A commit that carries the release tags of
  more than one version (`v1.1.4` and `v1.2.0`) has no one release to follow: the wait does not pick
  one, it ends at once with exit 11, `ambiguous_release`, naming every release tag on the commit, and
  a wait given one of those versions follows it. A candidate and the stable promoted onto its commit
  (`v1.2.0-rc.2`, `v1.2.0`) are one version, the stable followed; a tag outside the release pattern
  (an upstream `v1.3.0-alpha.1`) counts for nothing.

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

`ciModel` is `generated`, `hand-written`, `actions` or `none`, from the files at the tag: no
`.circleci/config.yml` is `none`, or `actions` when the tag's commit carries check runs (a repository whose CI is GitHub
Actions only: the check runs are the tag's pipeline, reported under `pipeline`, and the verdict is `available` only once
every one finished successfully, `ci_failed` when one failed); a `.circleci/workflows.yml` beside it is the signature of
`devctl gen circleci` and means `generated`; a `config.yml` alone is `hand-written`. The entry's
`gen.ci.generate` is cross-checked against it (`true` without the generated files, or an explicit
`false` with them, is exit 7). An `<image>/vX.Y.Z` tag of a `multi-image` entry cut by the hand-written
pipeline that preceded the generated one is the exception: the flavour names its image
`<owner>/<repo>/<image>:<X.Y.Z>` all the same, so the wait follows the tag's pipeline and probes that
image, `ciModel` `hand-written`, with a warning (`--image` or `--chart` reads the pipeline's jobs instead).

**Generated CI**: the artifacts are what devctl's generator emits for the entry, derived the way the
generator derives them (an entry without a `gen.ci` block declares no override, so the generator's
defaults name them; a repository no team file declares is exit 7, there being no entry to name them):

| Artifact | When | Name | Registry |
|---|---|---|---|
| image | a `Dockerfile` at the repository root of the tag, or `gen.ci.image.dockerfile` set | `gen.ci.image.name`, else `giantswarm/<repo>` | private with `gen.ci.image.privateOnly`, or for a private repository without `gen.ci.forcePublic`; public otherwise |
| chart | `gen.flavours` contains `app` and the repository is not a template | the `name` in `helm/<gen.ci.chartName>/Chart.yaml`, else in `helm/<repo>/Chart.yaml` | private for a private repository without `gen.ci.forcePublic`; public otherwise |

The chart's catalog is `gen.ci.appCatalog`, default `giantswarm-catalog`; its test catalog `gen.ci.appCatalogTest`,
default `giantswarm-test-catalog`.

`gen.ci.chartName` and a push job's `chart` parameter name the directory under `helm/` the pipeline
packages, not the chart: the architect orb packages that directory and `helm push` names the OCI
repository after the packaged chart. The chart is therefore probed, and looked for in the catalog index,
under the `name` its `Chart.yaml` declares at the tag; a repository renamed after its chart was created
keeps `helm/<old-name>` with `name: <repo>`. A `Chart.yaml` that is missing at the tag, does not parse or
declares no name is exit 7 naming the file; the directory is never probed in its place.

The table names what the generator renders; a repository adds jobs in `.circleci/custom.yml`, which the
setup workflow merges into the build workflow. Its architect push jobs that the tag pipeline runs are read
like those of hand-written CI (below) once the pipeline lists its jobs, and their images and charts are
probed by name too: giantswarm/agent-platform's connectivity chart, released off the same tag as the meta
chart, is part of the release. A custom job that pushes outside the architect orb (vm-manager's guest image)
is not probed by name; the release waits for it through the tag pipeline being green (below).

**Hand-written CI**: the artifacts are the push jobs the tag pipeline runs. The command reads the
`.circleci/config.yml` (and `workflows.yml`, `custom.yml` when present) at the tag, collects every
`<orb>/push-to-registries`, `push-to-registries-multiarch`, `push-to-docker` and
`push-to-app-catalog` job of every workflow with its parameters (`name`, `image`, `chart`,
`app_catalog`, `app_catalog_test`, `push`, `push_to_oci_registry`, `registries-data`, `force-public`), and keeps the
ones whose name CircleCI lists among the jobs of the tag pipeline's workflows. An image job without
`image` is the orb's default, `<owner>/<repo>`; a chart job's chart is the `name` in
`helm/<chart>/Chart.yaml` at the tag, as for generated CI; `push: false` and a chart job that pushes to
neither the catalog nor the registry are build-only and skipped; `registries-data` that names only
the private registry makes the image private. Because the pipeline's jobs are the source, the
command waits for the pipeline to exist before it knows the artifacts.

A tag job that pushes outside the architect orb (a plain `docker push` in a job of its own, as in a fork
whose upstream Dockerfile builds the binary) cannot be read from the configuration: the caller names
its image with `--image <owner>/<name>` (repeatable; the public or private registry host may come
first and then decides the registry, else the repository's visibility does). Each is expected as
`<image>:<git tag>`, the tag as written (`v2.43.3`, the `$CIRCLE_TAG` such a job pushes), beside what the push jobs name, and the release is out when every artifact is
pullable and the tag pipeline is green. `--image` applies to hand-written CI only: with generated CI or
none, the artifacts are derived as before and a warning says the named image is not probed.

A job the configuration defines itself whose commands run `helm push <package> oci://<target>` (the
substrate line's `charts` job) is read as well: every chart directory under `helm/` or `charts/` at the
tag whose directory or chart name the job's commands name is expected at `<target>/<chart>:<version>`,
the bare version, the job's `environment` and the plain `NAME=value` assignments of its commands filling
`$VAR` and `${VAR}` in the target (`oci://${REGISTRY}/helm` with `REGISTRY: gsoci.azurecr.io/giantswarm/substrate`),
when the job runs in the tag pipeline. A chart directory is one with a `Chart.yaml`, else a
`Chart-template.yaml` the build renders it from (the kagent line's `make helm-version`); the name either
declares is the published one. A job that runs `make <target>` instead is followed into the root
`Makefile` at the tag: the first `helm push` of the target's recipe, or of its prerequisites', names the
target, with the variables of the make command line over the Makefile's own (the kagent line's
`make helm-publish HELM_REPO="oci://${registry}/giantswarm"` pushes to `$(HELM_REPO)/kagent/helm`, so
its `kagent` and `kagent-crds` charts are expected at `giantswarm/kagent/helm/<chart>`). A chart at the
tag that no push job and no `--chart` covers, or whose job's target names neither registry once its
environment is read (a variable of a context, a Makefile default elsewhere), is unchecked: the
document's `warnings` name it and `--chart`.

A chart a job pushes with helm in a way the configuration and the Makefile do not show is named the same
way with `--chart <path>` (repeatable), its repository path in the registry:
`--chart giantswarm/kagent/helm/kagent`. It is expected as `<path>:<version>`, the bare version helm
requires (`1.2.4`), its chart name the path's last element; it goes to no app catalog, so `--catalog`
does not wait for it. `--chart` applies to hand-written CI only, like `--image`.

When a `Dockerfile` exists at the tag and neither a push job of the pipeline nor `--image` names an
image: exit 7 with the jobs seen, the reason naming `--image`. The repository name is never used as a
fallback.

**Neither image nor chart** (a CLI that ships binaries as release assets, a repository without
CircleCI): the release is available when the GitHub Release of the tag is published (not a draft) and
every workflow of the tag finished green. `artifacts` then lists the release assets with the digests
GitHub reports.

### Availability

A release is available when every artifact resolves to a digest, the tag's CI is green (the next
section) **and** every commit status of the tag's commit succeeded ([The commit's statuses](#the-commits-statuses)). The artifacts alone are not the release: a repository's own tag jobs push more than the sources
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
chart at the version; the index is fetched anew every time, so no cached copy answers. A pre-release
(`1.2.3-rc.1`) is looked for in the test catalog when the tag's CircleCI configuration pins the architect
orb at 10.12.0 or later, or at a dev version: from 10.12.0 the orb pushes a pre-release tag's chart to
`app_catalog_test`. An older pin still pushes it to the production catalog, which is then the one read.

### The tag's CI

Every poll reads the tag pipeline: the newest CircleCI pipeline whose `vcs.tag` is the tag, its
workflows reduced to the **newest run per workflow name** (a rerun, from failed or in full, is a
second workflow of the same name in the same pipeline, and the one it replaces keeps its failed
status for ever). Once a listing named the tag pipeline, a later one that no longer does (a busy
repository's newer pipelines push it off the pages read) leaves the wait reading that pipeline. A workflow in `failed`, `error`, `failing`, `canceled` or `unauthorized` ends the
wait with exit 1 and `pipeline.failedJobs` (`workflow/job`, or the workflow alone when CircleCI does
not list its jobs). The verdict is the workflows': their jobs are read only where they are needed,
to name the failed ones and to derive the artifacts of hand-written CI and of a `custom.yml` that
declares push jobs (one of test and repository-owned jobs alone names no artifact and needs no look
at them); the setup workflow's one job continues the pipeline and is never read. CircleCI answers 404 on the jobs of a workflow it lists, a finished one
included, for a while after the workflow exists and for as long as its job data lags behind (half
an hour and more during an incident), and a green pipeline is green without them; a derivation
that needs them goes on polling, `pipeline.unfinished` says `build (running, jobs not visible
yet)`, and jobs that never appear end the wait at its timeout naming the workflow. A repository without CircleCI is judged
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

`--failed-log` answers why the tag's CI failed, the code's or the CI provider's, without opening the
job in the browser, the same way as [`devctl pr wait --failed-log`](pr-wait.md) and with its log
readers: when the tag's CI failed it reads the log of each failed job once and keeps the last
`--failed-log-lines` lines (default 50), without terminal control sequences or control characters but tab. On CircleCI those are the failed
jobs of every failed workflow of the tag pipeline (the output of their failed steps), or the job a
failed CircleCI status links when the statuses are the verdict; for a repository without CircleCI,
the failed jobs of every failed Actions run of the tag (`GET /repos/{owner}/{repo}/actions/runs/{id}/jobs`,
then each job's log, ending at its last `##[error]` line). The lines from a log's first failure lead its tail
when that failure lies before it, as in [`pr wait`](pr-wait.md). The tails go to stderr, one block per job
headed `failed job <name> (<url>):`, and into the document under `failedJobs[]`. A green, pending or
timed-out wait reads no log; a log that cannot be read is named in its job's `logError`, and the
verdict stays exit 1. Without the flag the document is unchanged.

### The commit's statuses

The tag pipeline is not every status of the tag's commit: a late job, the branch pipeline that built
the same commit and other CIs report there too, and `devctl release promote` refuses a candidate as
`not_built` while their combined status is anything but `success`. Once every artifact is available
and the tag's CI is green, the wait reads the commit's statuses, the newest per context, as the
combined status judges them, and reports them in `statuses[]`:

- every one succeeded, or the commit carries none: the release is available, and a promote run right
  after accepts it;
- one is pending: the wait goes on, its heartbeat naming the context; at the deadline exit 2 names
  the pending contexts;
- one is `failure` or `error`: exit 12, `status_failed`, naming each failed context and its URL. The
  artifacts are out and the tag pipeline is green, so this is not the tag's CI failure (exit 1); a
  rerun that turns the context green is what the promote waits for.

### Polling

Requests to GitHub are conditional (`If-None-Match` with the last `ETag`), so an unchanged resource
costs a 304 that does not count against the rate limit. The interval between polls follows the
`X-RateLimit-Remaining` / `X-RateLimit-Reset` headers of the responses, between 15 and 60 seconds;
the rate_limit endpoint is never asked. `DEVCTL_TIME_SCALE` multiplies every sleep and the timeout
(the end-to-end tests run at 0.001). A GitHub or CircleCI read that fails in transit (a reset
connection, an EOF, a try over 60 s) or with a 5xx is sent again, up to fifteen tries in a row with a
pause from 2 s doubling to 60 s, each retried failure a warning with its time, as in
[`pr wait`](pr-wait.md#polling); a read that fails all fifteen is exit 7 naming the request and the count,
except a read of the tag's CI (the CircleCI pipeline, or the tag's Actions runs): an API that does not
answer says nothing about the release, so the next poll reads again until the timeout. A timeout after
such reads is exit 2 whose reason ends `the release is unknown, not failed: the last read of the tag's CI
went unanswered: <request and failure>`.
A read refused for a spent rate limit is sent again after the reset (or `Retry-After`), a warning
naming the limit and the reset time; a reset after the deadline is exit 2 at once, naming it.

Without `--progress` the wait is silent but for a heartbeat on stderr: `waiting for <what>` once every
two minutes, and at once when what it waits for changed (the artifacts not in their registry, the tag
pipeline's unfinished workflows, the tag, the release), so a wait that outlives its caller's patience
has named its cause; with `--progress` every poll says it.

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
| `command`, `schemaVersion`, `exitCode`, `verdict`, `reason`, `warnings`, `startedAt`, `finishedAt` | The envelope every agent-facing command prints. `verdict` is `available`, `ci_failed`, `timeout`, `not_applicable`, `no_release`, `ambiguous_release`, `status_failed`, `usage` or `auth_required`. `warnings` carries the CircleCI token's seven-day expiry notice and a team-file declaration that disagrees with the tag's workflows about the release model. |
| `repository`, `tag`, `sha` | What was waited for. `tag` is empty when it never appeared; `sha` is the tag's commit (with `--pr` the merge commit). |
| `releaseModel` | `auto-release` or `legacy`. |
| `ciModel` | `generated`, `hand-written`, `actions` or `none`. |
| `artifacts[]` | `kind` (`image`, `chart`, `release-asset`), `reference` (the pullable reference, or the asset's download URL), `digest` (empty while missing), `state` (`available`, `missing`). Empty until the artifacts are known (hand-written CI before its pipeline exists). |
| `pipeline` | The tag pipeline on CircleCI: `id`, `number`, `url`, `workflows[{name, status, conclusion}]` (newest run per name; for `ciModel` `actions` one entry per check run of the tag's commit, with its `conclusion`, `id`, `number` empty and `url` the commit's checks page), `failedJobs[]`, `unfinished[]` (the workflows not finished, `name (status)`, with `jobs not visible yet` when CircleCI does not list them yet; what a timeout was waiting for, empty at exit 0). When CircleCI names no pipeline for the tag and its commit's CircleCI statuses are the verdict, `workflows` are those statuses' jobs and `unfinished` the pending ones, with `id`, `number` and `url` empty. `null` for a repository without CircleCI and without check runs on the tag, or while the CircleCI pipeline does not exist. |
| `actions[]` | The Actions runs the tag triggered, for a repository without CircleCI: `name`, `runId`, `status`, `conclusion`, `url`. |
| `statuses[]` | The newest commit status of every context of the tag's commit, `context`, `state` (`pending`, `success`, `failure`, `error`) and `url`, read once the artifacts are available and the tag's CI is green; empty before. |
| `conflictingTags[]` | Present at exit 11: every release tag on the commit the wait resolved to, newest first. Absent otherwise. |
| `failedJobs[]` | Present at exit 1 with `--failed-log`, as in [`pr wait`](pr-wait.md#the-document): each failed job with `name` (`<workflow>/<job>` on CircleCI, `<run>/<job>` on Actions), `source` (`actions` or `circleci`), `url`, and `logTail`, the last `--failed-log-lines` lines of its log, led by the lines from its first failure when that lies before them, or `logError` when the log could not be read. A failed workflow or run whose jobs cannot be listed is named alone with its `logError`. Absent without the flag. |

## Exit codes

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `available` | Every artifact resolves to a digest and every workflow of the tag pipeline finished green (and, with `--catalog`, the index lists every chart); or the release of a repository without image and chart is published with its workflows green. |
| 1 | `ci_failed` | A workflow of the tag pipeline, or an Actions run of the tag, failed or was cancelled; with `--pr`, the merge commit's auto-release run failed before it tagged. `reason` and `pipeline.failedJobs` name the jobs. The release is incomplete until a rerun of the failed workflow succeeds or a fix lands as the next tag; artifacts that did resolve are listed `available`. |
| 2 | `timeout` | The deadline passed. `reason` names what is missing: the tag, the pipeline, the artifacts by reference, and the pipeline's unfinished workflows (also when every artifact is already available). |
| 3 | `not_applicable` | The pull request is not merged, or its auto-release run was cancelled before it tagged (superseded by a newer push), so `--pr` cannot resolve a version. |
| 3 | `no_release` | No release follows the pull request's merge: the repository does not tag merge commits (the legacy release model, or no release workflow at all), or the merge commit's auto-release run finished without a tag and no later tag on the pull request's base branch carries it; the reason names the branch. |
| 7 | `usage` | A bad argument or flag; a newer devctl released (the reason names `devctl version update`); no source says how the repository releases; the sources disagree about the CI model or the artifacts; a registry answer that is neither a digest nor "manifest unknown"; a tooling error, a GitHub or CircleCI read that failed eight tries in a row among them. |
| 8 | `auth_required` | No usable token in the keychain; `reason` names the `devctl auth login` invocation. |
| 11 | `ambiguous_release` | With `--pr`, the commit the wait resolves to (the merge commit, or the first tagged commit after it) carries the release tags (`vX.Y.Z`, `vX.Y.Z-rc.N`) of more than one version, or the `<image>/vX.Y.Z` tags of more than one image: the wait does not choose. `reason` and `conflictingTags` name every release tag on the commit, newest first; a wait given the version waits on it. A candidate and its promoted stable on one commit are one version. |
| 12 | `status_failed` | Every artifact is available and the tag's CI is green, and a commit status of the tag's commit (the branch pipeline's, another CI's) is `failure` or `error`: `devctl release promote` refuses the commit. `reason` and `statuses[]` name the failed contexts. |

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
| `DEVCTL_KEYRING_FILE` | unset | A JSON file that holds the records in place of the OS keychain; read from the environment when a command starts, created with mode 0600 on first write. Unset or empty keeps the keychain (tests, machines without a keychain daemon) |
| `DEVCTL_TIME_SCALE` | `1` | Multiplies every sleep and the timeout. |

## The requests, for a mock

The end-to-end harness (`e2e/README.md`) scripts these endpoints; a scenario of this command needs
the ones its path takes.

GitHub: `GET /repos/{o}/{r}/pulls/{n}` (with `--pr`), `GET /repos/{o}/{r}/tags` (with `--pr`, the
tag on the merge commit among the newest), `GET /repos/{o}/{r}/git/matching-refs/tags/` (with `--pr`,
every tag, when the newest do not include it), `GET /repos/{o}/{r}/compare/{sha}...{branch}` (with
`--pr`, after a run that tagged nothing: the base branch's commits since the merge), `GET /repos/{o}/{r}/git/ref/tags/{tag}` (and `GET
/repos/{o}/{r}/git/tags/{sha}` for an annotated tag), `GET /repos/{o}/{r}` (private or not), `GET
/repos/{o}/{r}/contents/` with `?ref=` (the root listing: the Dockerfile), `GET
/repos/{o}/{r}/contents/.github/workflows`, `GET /repos/{o}/{r}/contents/.circleci`, `GET
/repos/{o}/{r}/contents/.circleci/config.yml` (hand-written CI; `workflows.yml`, `custom.yml` when
listed; `custom.yml` for generated CI too), `GET /repos/{o}/{r}/contents/helm/{dir}/Chart.yaml` (the name of every chart), `GET /repos/giantswarm/github/contents/repositories` and `GET
/repos/giantswarm/github/contents/repositories/{team}.yaml` (the team-file entry, until the one that
declares the repository), `GET /repos/{o}/{r}/releases/tags/{tag}` and `GET
/repos/{o}/{r}/actions/runs?head_sha={sha}` (release assets, no CircleCI; with `--pr`, the merge
commit's auto-release run while no tag is on it). With `--failed-log` on a failed Actions run: `GET
/repos/{o}/{r}/actions/runs/{id}/jobs`, then `GET /repos/{o}/{r}/actions/jobs/{id}/logs` (a redirect
to the log, read without the token).

CircleCI: `GET /api/v2/project/gh/{o}/{r}/pipeline` (the tag pipeline by `vcs.tag`, newest pages),
`GET /api/v2/pipeline/{id}/workflow`, `GET /api/v2/workflow/{id}/job`. With `--failed-log` on a failed
job: `GET /api/v1.1/project/github/{o}/{r}/{job}` and its failed steps' `output_url`s, read without
the token.

Registries: `GET /v2/` then `HEAD /v2/{name}/manifests/{X.Y.Z}`, images under `giantswarm/…`, charts
under `charts/giantswarm/…`.
