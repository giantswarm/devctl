# MC reservations (`devctl reservation`)

A reservation points one app on one management cluster at the dev builds of your branch, for a limited time. When it ends, the app goes back to its normal release. Nobody else can deploy their branch of the same app there while you hold it.

Everything is a commit pushed to `main` of the GitOps repo, Flux does the rest a few minutes later. devctl never talks to the cluster, so the same commands work from a laptop and from CI.

## Before you start

- A checkout of the GitOps repo. By default devctl uses `$UMBRELLA_TARGET_DIR/cmc/giantswarm-management-clusters`, `--gitops-dir` points somewhere else. devctl never clones it.
- The checkout has to be on a clean `main` (untracked files are fine). Every command fetches and fast-forwards first, so you always work on the newest state. If you have local commits, devctl refuses and tells you how to reset.
- Your git identity (`git config user.name` and `user.email`). Commits are made as you, `--user` never changes that, it only says who holds the reservation.
- `--user` defaults to the login of your GitHub token (`DEVCTL_GITHUB_TOKEN`, `GITHUB_TOKEN`, `OPSCTL_GITHUB_TOKEN`, or the `devctl auth login --github-only` login). With `--user` set, no token is needed. A GitHub App installation token has no login, so CI passes `--user`.
- The cluster has to be enabled for reservations: it needs `configmap-reservations.yaml` with a `reservations` ConfigMap. If it's not there, `reserve` tells you exactly what to add.

## Reserve

```
devctl reservation reserve --cluster graveler --app hello-world --branch fix/crash \
  --pull-request giantswarm/hello-world#123 --user alice --duration 4h
```

- `--branch` is required, it selects the dev builds to follow.
- `--pull-request` is optional. Without it you get a warning: devctl doesn't check that the branch has builds, and `extend`/`release --pull-request` won't find this reservation.
- `--app` can be skipped when you run it in the app repo, the chart name comes from `helm/*/Chart.yaml`.
- `--duration` is `30m`, `4h`, `4h30m` or `2d`: a whole number of minutes in Go duration form, or a whole number of days. Default is 10h, max is 7d or less, if the cluster sets its own `reservations.giantswarm.io/max-duration`.
- `--exclusive` locks the whole cluster, not just the app. If you already hold the app there, it's promoted in place.

Running the same `reserve` again (same app, user, branch, PR and scope) just refreshes it: a new window starting now. Anything else that collides is refused with who holds it, on which branch and until when.

## Check what's reserved

```
devctl reservation list --cluster graveler
```

It reads the tip of `main`, not the cluster. Each reservation shows as `active` until it expires. After that it shows as `expired` until the reaper removes it; an expired one doesn't block anyone.

## Extend

```
devctl reservation extend --pull-request giantswarm/hello-world#123 --user alice
```

Restarts the window of every reservation that PR holds, with the same duration as before. `--user` becomes the holder, so you can take over a reservation someone else made. An expired reservation can't be extended, reserve it again.

## Release

```
devctl reservation release --pull-request giantswarm/hello-world#123 --user alice
devctl reservation release --cluster graveler --app hello-world --user alice
```

The first one releases everything the PR holds on every cluster; if the PR holds nothing, it says so and exits 0. The second one releases a single app, and it fails if the app holds no reservation there. Use the second form also to free a stuck reservation by hand.

## Reap

```
devctl reservation reap --user reservation-reaper
```

Releases every expired reservation on every enabled cluster. With a GitHub token (the first set of `DEVCTL_GITHUB_TOKEN`, `GITHUB_TOKEN` and `OPSCTL_GITHUB_TOKEN`, else the `devctl auth login --github-only` login, never under CI) it also releases reservations whose PR is closed or merged (in case the close event got lost), and reservations whose PR branch was renamed, as the old branch doesn't build anything anymore. Without a token it warns and skips these checks.

## Output

By default (`--output table`) `reserve` prints one line: what, where, for whom and until when. `--log-level debug` also shows the source object, the version filter and the commit. `extend`, `reap` and `release --pull-request` print one tab-separated line per change. `list` prints a table.

`--output json` prints the same data as JSON, for scripts. `reserve` and `release --cluster` print one object. `extend`, `reap`, `release --pull-request` and `list` print an array, `[]` when there is nothing.

## Dry run

`--dry-run` works on `reserve`, `release`, `extend` and `reap`. devctl makes the commits in your checkout as usual, shows them on stderr (`git log -p`), then resets the checkout to `origin/main`. Nothing is pushed. The output on stdout is what a real run would print, except that the commits exist only in the dry run.
