# Waiting for a rollout: `devctl rollout wait`

```nohighlight
devctl rollout wait <installation> <owner/repo> (<vX.Y.Z | X.Y.Z> | --pr <number>) [--context <kube-context>]
    [--timeout 30m] [--release-timeout 30m] [--image <owner/name>]... [--helmrelease [<namespace>/]<name>]...
    [--reconcile] [--progress]
```

Blocks until a release runs on an installation, then prints one JSON document and exits with a code
that says what happened. It is the last step after [`devctl pr merge`](pr-merge.md) and
[`devctl release wait`](release-wait.md): the release is pullable, and now Flux (or app-operator) on the
installation's management cluster has to pick it up, deploy it and roll its workloads out. An agent
runs this one command instead of polling `kubectl get helmrelease` in a loop.

```nohighlight
devctl rollout wait myinstallation giantswarm/app-operator v7.5.4
devctl rollout wait myinstallation giantswarm/app-operator --pr 1234 --reconcile --progress
```

## What the command reads, and why

### The release first

The command starts with the wait of `devctl release wait` for the same version or `--pr`, with the
same rules, tokens and exit codes (`--release-timeout`, 30 minutes by default; `--image` names the image of a hand-written tag job that pushes outside the architect orb, as there). That wait names the
release's charts from the sources that define them (the team-file entry, the tag pipeline's push
jobs, `helm/<dir>/Chart.yaml` at the tag), so a repository that publishes several charts is waited for
on all of them (a second chart a generated pipeline's `custom.yml` pushes off the same tag included:
giantswarm/agent-platform's connectivity chart, whose HelmRelease upgrades minutes after the meta
chart's), and a chart named differently from its repository is found. A release that ships no
chart is exit 3. The document's `release` carries that wait's verdict, reason and result.

### The installation

The management cluster is read through the kube context `tsh kube login <installation>` writes,
`teleport.giantswarm.io-<installation>` (`--context` names another), as you. A context that does not
exist is exit 7; one that is not signed in, or whose credentials expired, is exit 8 naming
`tsh kube login`. Reads that fail in transit or with a 5xx are retried like every other agent-facing
command's.

A chart is deployed by one of:

| Object | Matched when | Running version |
|---|---|---|
| Flux `HelmRelease` (`helm.toolkit.fluxcd.io/v2`) | its `spec.chartRef` names an `OCIRepository` whose URL ends in the chart's name (any registry, so a mirror counts) or a `HelmChart` of the chart, or its `spec.chart.spec.chart` is the chart | the newest `status.history` entry with status `deployed`, build metadata stripped |
| `App` (`application.giantswarm.io/v1alpha1`) | its `spec.name` is the chart | `status.version` with `status.release.status` `deployed` |

Every match on the cluster is judged; none is exit 3. `--helmrelease <name>` or
`--helmrelease <namespace>/<name>` (repeatable) adds a HelmRelease whatever chart it deploys, judged
the same way against the version; a name no HelmRelease on the cluster answers to is exit 3.

### When a deployment has rolled out

It runs the version or a newer one (a newer one with a warning), the HelmRelease is Ready at its
current generation, and every Deployment, StatefulSet and DaemonSet of its Helm release (Helm's
`meta.helm.sh/release-name` and `release-namespace` annotations, in the release namespace) is rolled
out the way `kubectl rollout status` judges it: the new generation observed, every replica updated,
no old replica left, the updated ones available (a StatefulSet: ready and at the update revision or
above its partition; a DaemonSet: every scheduled pod updated and available; `OnDelete` counts as
rolled out). The workloads of a HelmRelease with `spec.kubeConfig` or of an App into a workload cluster
live elsewhere and are not read; a warning says so.

### When a deployment will not get there

A deployment whose source does not admit the version is reported with the state `not_following` and a
warning, and the wait goes on for the others: an OCIRepository pinned to another tag or to a digest,
a semver range (the OCIRepository's `spec.ref.semver`, the HelmChart's or `spec.chart.spec.version`)
that excludes it, an App whose `spec.version` is older (a workload cluster's App pinned to an older
release). A suspended HelmRelease is `suspended`, likewise. When no deployment follows the version,
the wait is exit 3.

### When a rollout failed

The wait ends at once with exit 1 when the version was attempted and failed: the HelmRelease's
`lastAttemptedRevision` is the version and it is `Stalled`, or not Ready with `InstallFailed`,
`UpgradeFailed`, `TestFailed`, `RollbackSucceeded` or `UninstallSucceeded`; the App of the version
reports a `failed` release; a Deployment of the release is past its progress deadline. A failure of an
older revision is not the version's: the wait goes on.

### Polling and `--reconcile`

The cluster is listed after a pause of 5 seconds, doubling to 30, until `--timeout` (30 minutes by
default) after the release was available. Flux polls an OCIRepository at its own interval, often ten
minutes; `--reconcile` sets `reconcile.fluxcd.io/requestedAt` once on the source and the HelmRelease
of each deployment still behind, the annotation `flux reconcile` sets, so Flux looks now. It is the
only write the command makes; without it the command only reads.

## The document

The envelope (`command`, `schemaVersion`, `exitCode`, `verdict`, `reason`, `warnings`, `startedAt`,
`finishedAt`) and:

| Field | Meaning |
|---|---|
| `installation`, `context` | The installation and the kube context it was read through. |
| `version` | The version waited for, bare. |
| `charts` | The release's charts, the names deployments are matched by. |
| `deployments[]` | Every HelmRelease and App of the charts at the last poll: `kind`, `namespace`, `name`, `chart`, `source` (`OCIRepository <ns>/<name>`, `HelmChart <ns>/<name>`, `catalog <name>`), `follows` (`semver <range>`, `tag <tag>`, `digest <digest>`, `version <v>`), `runningVersion`, `state` (`rolled_out`, `progressing`, `failed`, `not_following`, `suspended`), `message` (what it waits for or why it failed) and `workloads[]` (`kind`, `namespace`, `name`, `ready`, `message`). |
| `release` | The release wait's `verdict` and `reason`, and its result fields (see [release-wait.md](release-wait.md#the-document)). |

## Exit codes

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `rolled_out` | Every deployment of the charts that follows the version runs it or a newer one, ready and rolled out. |
| 1 | `rollout_failed`, `ci_failed` | The rollout failed (the reason names the deployment and its condition), or the release's CI did. |
| 2 | `timeout` | `--release-timeout` or `--timeout` passed; `deployments[].message` says what is missing. |
| 3 | `not_applicable`, `no_release` | No release follows the pull request, the release ships no chart, nothing on the installation deploys its charts, no deployment follows the version, or no HelmRelease answers to a `--helmrelease` name. |
| 7 | `usage` | A bad argument, a kube context that does not exist, a request the cluster refused. |
| 8 | `auth_required` | No GitHub token (`devctl auth login`), or the kube context is not signed in (`tsh kube login <installation>`). |
