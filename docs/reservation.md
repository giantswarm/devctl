# MC reservations (`devctl reservation`)

A reservation points one app on one management cluster at the dev builds of your branch, for a limited time. When it ends, the app goes back to its normal release. Nobody else can deploy their branch of the same app there while you hold it.

Everything is a commit pushed to `main` of the GitOps repo, Flux does the rest a few minutes later. devctl never talks to the cluster, so the same commands work from a laptop and from CI. In CI, the PR comments (`/deploy`, `/extend`, ...) call these commands for you, so most of the time you don't need to run them by hand.

## Before you start

- A checkout of the GitOps repo. By default devctl uses `$UMBRELLA_TARGET_DIR/cmc/giantswarm-management-clusters`, `--gitops-dir` points somewhere else. devctl never clones it.
- The checkout has to be on a clean `main` (untracked files are fine). Every command fetches and fast-forwards first, so you always work on the newest state. If you have local commits, devctl refuses and tells you how to reset.
- Your git identity (`git config user.name` and `user.email`). Commits are made as you, `--user` never changes that, it only says who holds the reservation. In CI, the workflow sets the bot identity.
- The cluster has to be enabled for reservations: it needs `configmap-reservations.yaml` with a `reservations` ConfigMap. If it's not there, `reserve` tells you exactly what to add.

## Reserve

```
devctl reservation reserve --cluster graveler --app hello-world --branch fix/crash \
  --pull-request giantswarm/hello-world#123 --user alice --duration 4h
```

- `--branch` is required, it selects the dev builds to follow.
- `--pull-request` is optional for local runs (CI always sets it). Without it you get a warning: devctl doesn't check that the branch has builds, and `extend`/`release --pull-request` won't find this reservation.
- `--app` can be skipped when you run it in the app repo, the chart name comes from `helm/*/Chart.yaml`.
- `--duration` is `30m`, `4h`, `2d`. Default is 10h, max is 7d or less, if the cluster sets its own `reservations.giantswarm.io/max-duration`.
- `--exclusive` locks the whole cluster, not just the app. If you already hold the app there, it's promoted in place.

Running the same `reserve` again (same app, user, branch, PR and scope) just refreshes it: a new window starting now. Anything else that collides is refused with who holds it, on which branch and until when.

## Check what's reserved

```
devctl reservation list --cluster graveler
```

It reads the tip of `main`, not the cluster. Expired reservations that the reaper didn't remove yet show as `expired`; they don't block anyone.

## Extend

```
devctl reservation extend --pull-request giantswarm/hello-world#123 --user alice
```

Restarts the window of every reservation that PR holds, with the same duration as before. `--user` becomes the holder, so you can take over a reservation CI made. An expired reservation can't be extended, reserve it again.

## Release

```
devctl reservation release --pull-request giantswarm/hello-world#123 --user alice
devctl reservation release --cluster graveler --app hello-world --user alice
```

The first one releases everything the PR holds on every cluster, the second one a single app. If there's nothing to release, it says so and exits 0. Use the second form also to free a stuck reservation by hand.

## Reap

```
devctl reservation reap --user reservation-reaper
```

Releases every expired reservation on every enabled cluster. With a GitHub token (`DEVCTL_GITHUB_TOKEN`, `GITHUB_TOKEN`, `OPSCTL_GITHUB_TOKEN` or `devctl auth login`) it also releases reservations whose PR branch was renamed, as the old branch doesn't build anything anymore. Without a token it warns and skips that check. CI runs it on a schedule, you only need it locally if the schedule is broken.

## Output

By default `reserve` prints one line: what, where, for whom and until when. `--log-level debug` also shows the source object, the version filter and the commit. `extend` and `reap` print one tab-separated line per change, so scripts can parse them.
