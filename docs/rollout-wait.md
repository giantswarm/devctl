# Waiting for a rollout: `devctl rollout wait`

```nohighlight
devctl rollout wait <installation> <owner/repo> (<vX.Y.Z | X.Y.Z> | --pr <number>) [--context <kube-context>] [--kubeconfig <file>]
    [--timeout 30m] [--release-timeout 30m] [--image <owner/name>]... [--chart <path>]... [--helmrelease [<namespace>/]<name>]...
    [--reconcile] [--progress] [--failed-log [--failed-log-lines 50]]
```

Blocks until a release runs on an installation, then prints one JSON document and exits with a code
that says what happened. It is the last step after [`devctl pr merge`](pr-merge.md) and
[`devctl release wait`](release-wait.md): the release is pullable, and now Flux (or app-operator) on the
installation's management cluster has to pick it up, deploy it and roll its workloads out. An agent
runs this one command instead of polling `kubectl get helmrelease` in a loop.

```nohighlight
devctl rollout wait myinstallation giantswarm/app-operator v7.5.4
devctl rollout wait myinstallation giantswarm/app-operator --pr 1234 --reconcile --progress
devctl rollout wait myinstallation giantswarm/kagent-upstream v1.2.4 --chart giantswarm/kagent/helm/kagent --chart giantswarm/kagent/helm/kagent-crds
devctl rollout wait myinstallation giantswarm/giantswarm-configs --pr 1234
```

## What the command reads, and why

### The release first

The command starts with the wait of `devctl release wait` for the same version or `--pr`, with the
same rules, tokens and exit codes (`--release-timeout`, 30 minutes by default; `--image` and `--chart` name the image and the chart a hand-written tag job pushes outside the architect orb, as there: the kagent line's charts under `giantswarm/kagent/helm/`; `--failed-log` and `--failed-log-lines` read the tail of each failed job's log when the release's CI failed, as there and as in [`devctl pr wait`](pr-wait.md), into `release.failedJobs[]` and stderr). That wait names the
release's charts from the sources that define them (the team-file entry, the tag pipeline's push
jobs, `helm/<dir>/Chart.yaml` at the tag), so a repository that publishes several charts is waited for
on all of them (a second chart a generated pipeline's `custom.yml` pushes off the same tag included:
giantswarm/agent-platform's connectivity chart, whose HelmRelease upgrades minutes after the meta
chart's), and a chart named differently from its repository is found. A release that ships no
chart is exit 3; for hand-written CI the reason names `--chart`. The document's `release` carries that
wait's verdict, reason and result.

### A pull request on a release candidate: release, then promote

A repository that releases in two steps tags the merge commit with a release candidate (`v1.2.3-rc.1`, a
GitHub pre-release) and cuts the stable release later with `devctl release promote`. An installation that
follows stable releases (`semver: ">=0.0.0"`) never deploys the candidate, so `--pr` follows the promotion:
the lowest published stable release above the candidate whose tag contains the merge commit (GitHub's
comparison) is waited for, `release.tag` names it and `release.candidate` the candidate it was promoted
from. Before the promote the candidate is waited for, with `release.promotePending` true; when nothing on
the installation follows a pre-release the command ends with exit 3 and a reason that names the candidate
and the pending promote. Run it again after the promote.

### A pull request that releases nothing: a configuration change

When `--pr` names a pull request no release follows (a giantswarm-configs change: the release wait answers
`no_release`), the command follows its merge commit on the installation instead, and `revision` carries it:

| Object | Followed when | Rolled out when |
|---|---|---|
| Flux `GitRepository` | its `spec.url` is the repository (ssh or https) | its artifact's commit is the merge commit or contains it (GitHub's comparison) |
| Flux `Kustomization` | its `spec.sourceRef` is one of those GitRepositories | `lastAppliedRevision` contains the merge commit, Ready at its generation |
| `Konfiguration` (`konfigure.giantswarm.io`) | its `spec.sources.flux.gitRepository` is one of them | `lastAppliedRevision` contains the merge commit, Ready |
| Flux `HelmRelease` | its `valuesFrom` takes the ConfigMap or Secret a Konfiguration renders for an app the pull request changed (`installations/<installation>/apps/<app>/`, `default/apps/<app>/`), or `--helmrelease` names it | the rendered ConfigMap's `configuration.giantswarm.io/revision` contains the merge commit, the HelmRelease deployed after that ConfigMap was written, took values other than its previous deploy's (`status.history[].configDigest`) after the pull request merged, deployed only after the merge as far as `status.history` keeps (the deploy that took the change evicted), or handled a `--reconcile` request made after the ConfigMap was written; Ready, its workloads rolled out |
| Flux `HelmRelease` of a Kustomization | one of those Kustomizations changed it since the pull request merged (its `status.inventory` holds it and kustomize-controller's field-manager entry is newer than the merge: a chart range or another spec field), changed a ConfigMap or Secret of its `valuesFrom`, or changed the OCIRepository or HelmChart its `spec.chartRef` names; or `--helmrelease` names it | a changed HelmRelease: helm-controller observed its new generation, Ready with no reconcile in flight, its workloads rolled out; changed values or chart source: the HelmRelease deployed no earlier than kustomize-controller wrote them (`source` names them, `follows` the Kustomization) or handled a `--reconcile` request, Ready, its workloads rolled out; a failed install, upgrade or test since is exit 1; a named HelmRelease nothing changed: Ready at its generation, its workloads rolled out |
| `App` (`application.giantswarm.io/v1alpha1`) | its `spec.config` or `spec.userConfig` names the ConfigMap or Secret a Konfiguration renders for an app the pull request changed | the rendered ConfigMap contains the merge commit, the App's `status.release.lastDeployed` is after that ConfigMap was written or, for an App in the management cluster, after app-operator last changed the values it merges for the App's Chart, which it did after the pull request merged; its release `deployed` at its `spec.version` (`runningVersion`), its workloads rolled out |
| `ConfigMap` | a Konfiguration renders it for an app the pull request changed and no HelmRelease or App takes it | never: `state` `unknown`, whether the change runs cannot be told |

A HelmRelease upgrades only when its rendered values changed, which only the values tell: a pull request
that changes files beyond one app's (`installations/<installation>/config.yaml.patch`, `default/config.yaml`)
follows no HelmRelease by itself, with a warning; `--helmrelease` names one to follow. Another
installation's files change nothing here. konfigure-operator re-labels a rendered ConfigMap with every
revision it applies, so its last write says when it was last rendered, not when its values changed: a
deployment that took new values after the merge runs the change although the ConfigMap was written since
(an older revision konfigure-operator renders after the merge, within its fetch interval, is not told
apart). kustomize-controller writes an object only when the repository changed it, so its field-manager entry
says when the change landed: a pull request that changes no HelmRelease, values or chart source returns once
the source and the Kustomizations applied it. A Kustomization or Konfiguration whose attempt at the revision
failed, or an App whose release failed with the new values, is exit 1; once everything else is applied, a
changed app whose values nothing takes is exit 3, verdict `rollout_unknown`, never `rolled_out`; nothing on the installation fetching the repository keeps the `no_release` verdict, exit
3, its reason saying so; a GitRepository nothing reads (one included into another) is exit 3. `--reconcile`
asks the GitRepository, the Kustomizations and, once its values are rendered, each HelmRelease to
reconcile, once each.

### The installation

The management cluster is read through the kube context `tsh kube login <installation>` writes,
`teleport.giantswarm.io-<installation>`, as you. An installation reached through another context
(`kubectl gs login`'s `gs-<installation>`, a kind lab) is read through the one `--context` names. The
contexts come from the default kubeconfig (`KUBECONFIG`, `~/.kube/config`), or from the one file
`--kubeconfig <file>` names, for both the context lookup and the reads; neither is ever written.

The context is checked before the release wait. When the kubeconfig lacks the Teleport context and `tsh`
is logged in (`tsh status` shows a profile that has not expired), the wait runs
`tsh kube login <installation>` with `KUBECONFIG` set to a private temporary file for that one command,
reads the installation through it and removes the file before the release wait starts: a session needs
no login of its own and the machine's kubeconfig stays as it was. A `tsh` that is not logged in, or
whose profile expired, is exit 8 naming `tsh login`; the wait never starts an interactive login. Any
other missing context (one `--context` names, or the Teleport one without `tsh` installed) is exit 7 at
once, its reason naming the kubeconfig's contexts that mention the installation and `--context`, as is a
`tsh kube login` that fails (an installation Teleport does not know). A context that is not signed in,
or whose credentials expired, is exit 8 naming `tsh kube login`. Reads that fail in transit or with a
5xx are retried like every other agent-facing command's.

A chart is deployed by one of:

| Object | Matched when | Running version |
|---|---|---|
| Flux `HelmRelease` (`helm.toolkit.fluxcd.io/v2`) | its `spec.chartRef` names an `OCIRepository` whose URL ends in the chart's name (any registry, so a mirror counts) or a `HelmChart` of the chart, or its `spec.chart.spec.chart` is the chart | the newest `status.history` entry with status `deployed`, build metadata stripped |
| `App` (`application.giantswarm.io/v1alpha1`) | its `spec.name` is the chart | `status.version` with `status.release.status` `deployed` |

Every match on the cluster is judged; none is exit 3. `--helmrelease <name>` or
`--helmrelease <namespace>/<name>` (repeatable) adds a HelmRelease whatever chart it deploys; a name no
HelmRelease on the cluster answers to is exit 3, the reason naming how many HelmReleases it looked through, in
all namespaces of the context. One of the release's charts is judged the same way against
the version. One of none of them (a component a meta chart renders, on a version line of its own) is judged
by itself: rolled out once it is Ready at its current generation (`status.observedGeneration`) with no
reconcile in flight (`Reconciling` not True) and its workloads are rolled out; `Progressing` or
`DependencyNotReady` keeps the wait open; Stalled, or not Ready after a failed install, upgrade or test, is
exit 1. `--reconcile` does not ask it to reconcile.

### When a deployment has rolled out

It runs the version or a newer one (a newer one with a warning), the HelmRelease is Ready at its
current generation, and every Deployment, StatefulSet, DaemonSet and Flux HelmRelease of its Helm release (Helm's
`meta.helm.sh/release-name` and `release-namespace` annotations, in any namespace: kagent's chart
renders its controller into `kagent` from a release in `agent-platform`) is rolled
out the way `kubectl rollout status` judges it: the new generation observed, every replica updated,
no old replica left, the updated ones available (a StatefulSet: ready and at the update revision or
above its partition; a DaemonSet: every scheduled pod updated and available; `OnDelete` counts as
rolled out; a HelmRelease a meta chart renders: its generation observed, Ready with no reconcile in flight,
its dependencies included, a suspended one counted, Stalled or a failed install, upgrade or test exit 1). The workloads of a HelmRelease with `spec.kubeConfig` or of an App into a workload cluster
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
`finishedAt`; `warnings` holds what the last poll found, a warning seen mid-roll and gone by the end is only
shown as progress) and:

| Field | Meaning |
|---|---|
| `installation`, `context` | The installation and the kube context it was read through. |
| `version` | The version waited for, bare; empty for a configuration change. |
| `revision` | The merge commit of a configuration change waited for; absent for a version. |
| `charts` | The release's charts, the names deployments are matched by. |
| `deployments[]` | Every HelmRelease and App of the charts at the last poll (for a configuration change, the GitRepositories, Kustomizations, Konfigurations, HelmReleases, Apps and ConfigMaps above, `runningVersion` the commit each applied or the chart version): `kind`, `namespace`, `name`, `chart`, `source` (`OCIRepository <ns>/<name>`, `HelmChart <ns>/<name>`, `catalog <name>`), `follows` (`semver <range>`, `tag <tag>`, `digest <digest>`, `version <v>`), `runningVersion`, `state` (`rolled_out`, `progressing`, `failed`, `not_following`, `suspended`, `unknown`), `message` (what it waits for or why it failed) and `workloads[]` (`kind`, `namespace`, `name`, `ready`, `message`). |
| `release` | The release wait's `verdict` and `reason`, and its result fields (see [release-wait.md](release-wait.md#the-document)); with `--failed-log`, `release.failedJobs[]` when the release's CI failed. |

## Exit codes

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `rolled_out` | Every deployment of the charts that follows the version runs it or a newer one, ready and rolled out; for a configuration change, everything above applied the merge commit. |
| 1 | `rollout_failed`, `ci_failed` | The rollout failed (the reason names the deployment and its condition), or the release's CI did. |
| 2 | `timeout` | `--release-timeout` or `--timeout` passed; `deployments[].message` says what is missing. |
| 3 | `not_applicable`, `no_release`, `rollout_unknown` | No release follows the pull request and nothing on the installation fetches its repository, nothing reads the GitRepository that does, the release ships no chart, nothing on the installation deploys its charts, no deployment follows the version, or no HelmRelease answers to a `--helmrelease` name; `rollout_unknown`: a configuration change is applied and no HelmRelease or App takes the values of an app it changed, so whether it runs cannot be told. |
| 7 | `usage` | A bad argument, a kube context that does not exist and `tsh` could not log in to, a `--kubeconfig` that cannot be read, a request the cluster refused. |
| 8 | `auth_required` | No GitHub token (`devctl auth login`), the kube context is not signed in (`tsh kube login <installation>`), or it is missing and `tsh` is not logged in (`tsh login`). |
| 11 | `ambiguous_release` | With `--pr`, the release's commit carries the release tags of more than one version, as in [`release wait`](release-wait.md#exit-codes); `release.conflictingTags` names them, and the wait given the version follows it. |
| 12 | `status_failed` | The release's artifacts are available and its tag pipeline is green, and a commit status of its commit failed, as in [`release wait`](release-wait.md#exit-codes); the reason names the failed contexts. |
