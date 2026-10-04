# Waiting for a rollout: `devctl rollout wait`

```nohighlight
devctl rollout wait <installation> <owner/repo> (<vX.Y.Z | X.Y.Z> | --pr <number>) [--context <kube-context>]
    [--timeout 30m] [--release-timeout 30m] [--image <owner/name>]... [--chart <path>]... [--helmrelease [<namespace>/]<name>]...
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
/home/teemow/.go/bin/beekeeper gate -- devctl rollout wait myinstallation giantswarm/kagent-upstream v1.2.4 --chart giantswarm/kagent/helm/kagent --chart giantswarm/kagent/helm/kagent-crds
/home/teemow/.go/bin/beekeeper gate -- devctl rollout wait myinstallation giantswarm/giantswarm-configs --pr 1234
```

## What the command reads, and why

### The release first

The command starts with the wait of `devctl release wait` for the same version or `--pr`, with the
same rules, tokens and exit codes (`--release-timeout`, 30 minutes by default; `--image` and `--chart` name the image and the chart a hand-written tag job pushes outside the architect orb, as there: the kagent line's charts under `giantswarm/kagent/helm/`). That wait names the
release's charts from the sources that define them (the team-file entry, the tag pipeline's push
jobs, `helm/<dir>/Chart.yaml` at the tag), so a repository that publishes several charts is waited for
on all of them (a second chart a generated pipeline's `custom.yml` pushes off the same tag included:
giantswarm/agent-platform's connectivity chart, whose HelmRelease upgrades minutes after the meta
chart's), and a chart named differently from its repository is found. A release that ships no
chart is exit 3; for hand-written CI the reason names `--chart`. The document's `release` carries that
wait's verdict, reason and result.

### A pull request that releases nothing: a configuration change

When `--pr` names a pull request no release follows (a giantswarm-configs change: the release wait answers
`no_release`), the command follows its merge commit on the installation instead, and `revision` carries it:

| Object | Followed when | Rolled out when |
|---|---|---|
| Flux `GitRepository` | its `spec.url` is the repository (ssh or https) | its artifact's commit is the merge commit or contains it (GitHub's comparison) |
| Flux `Kustomization` | its `spec.sourceRef` is one of those GitRepositories | `lastAppliedRevision` contains the merge commit, Ready at its generation |
| `Konfiguration` (`konfigure.giantswarm.io`) | its `spec.sources.flux.gitRepository` is one of them | `lastAppliedRevision` contains the merge commit, Ready |
| Flux `HelmRelease` | its `valuesFrom` takes the ConfigMap or Secret a Konfiguration renders for an app the pull request changed (`installations/<installation>/apps/<app>/`, `default/apps/<app>/`), or `--helmrelease` names it | the rendered ConfigMap's `configuration.giantswarm.io/revision` contains the merge commit, the HelmRelease deployed after that ConfigMap was written (or handled a `--reconcile` request made after it), Ready, its workloads rolled out |

A HelmRelease upgrades only when its rendered values changed, which only the values tell: a pull request
that changes files beyond one app's (`installations/<installation>/config.yaml.patch`, `default/config.yaml`)
follows no HelmRelease by itself, with a warning; `--helmrelease` names one to follow. Another
installation's files change nothing here. A Kustomization or Konfiguration whose attempt at the revision
failed is exit 1; nothing on the installation fetching the repository keeps the `no_release` verdict, exit
3, its reason saying so; a GitRepository nothing reads (one included into another) is exit 3. `--reconcile`
asks the GitRepository, the Kustomizations and, once its values are rendered, each HelmRelease to
reconcile, once each.

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
`meta.helm.sh/release-name` and `release-namespace` annotations, in any namespace: kagent's chart
renders its controller into `kagent` from a release in `agent-platform`) is rolled
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
| `version` | The version waited for, bare; empty for a configuration change. |
| `revision` | The merge commit of a configuration change waited for; absent for a version. |
| `charts` | The release's charts, the names deployments are matched by. |
| `deployments[]` | Every HelmRelease and App of the charts at the last poll (for a configuration change, the GitRepositories, Kustomizations, Konfigurations and HelmReleases above, `runningVersion` the commit each applied or the chart version): `kind`, `namespace`, `name`, `chart`, `source` (`OCIRepository <ns>/<name>`, `HelmChart <ns>/<name>`, `catalog <name>`), `follows` (`semver <range>`, `tag <tag>`, `digest <digest>`, `version <v>`), `runningVersion`, `state` (`rolled_out`, `progressing`, `failed`, `not_following`, `suspended`), `message` (what it waits for or why it failed) and `workloads[]` (`kind`, `namespace`, `name`, `ready`, `message`). |
| `release` | The release wait's `verdict` and `reason`, and its result fields (see [release-wait.md](release-wait.md#the-document)). |

## Exit codes

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `rolled_out` | Every deployment of the charts that follows the version runs it or a newer one, ready and rolled out; for a configuration change, everything above applied the merge commit. |
| 1 | `rollout_failed`, `ci_failed` | The rollout failed (the reason names the deployment and its condition), or the release's CI did. |
| 2 | `timeout` | `--release-timeout` or `--timeout` passed; `deployments[].message` says what is missing. |
| 3 | `not_applicable`, `no_release` | No release follows the pull request and nothing on the installation fetches its repository, nothing reads the GitRepository that does, the release ships no chart, nothing on the installation deploys its charts, no deployment follows the version, or no HelmRelease answers to a `--helmrelease` name. |
| 7 | `usage` | A bad argument, a kube context that does not exist, a request the cluster refused. |
| 8 | `auth_required` | No GitHub token (`devctl auth login`), or the kube context is not signed in (`tsh kube login <installation>`). |
