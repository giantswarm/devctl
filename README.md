[![CircleCI](https://dl.circleci.com/status-badge/img/gh/giantswarm/devctl/tree/main.svg?style=svg)](https://dl.circleci.com/status-badge/redirect/gh/giantswarm/devctl/tree/main)

# devctl

`devctl` is a command-line tool designed to streamline development workflows at Giant Swarm. It provides various commands to help manage repositories and generate files.

## Installation

> **Important**: We recommend downloading the latest release from our [releases page](https://github.com/giantswarm/devctl/releases) rather than using `go install`. This ensures you get a properly built binary with:
> - Correct version information
> - Git commit information for traceability
> - Build timestamps
> - All necessary build flags
> - Generated code and mocks for testing
>
> While `go install` will work, it won't include this important metadata and may miss generated code that helps with debugging and version tracking.

```bash
# Not recommended
go install github.com/giantswarm/devctl/v7@latest
```

**Recommended**: Download the latest release from

https://github.com/giantswarm/devctl/releases

## Features

### Authentication for the agent-facing commands (`devctl auth`)

`devctl auth login` logs in to GitHub (the device flow of the devctl GitHub App, refreshed without a human) and CircleCI (OAuth 2.0 with PKCE and dynamic client registration, a 90-day token) and keeps both tokens in the OS keychain; `devctl auth login --muster-only` signs in to muster the same way, for the `repo` commands that call giantswarm-repo-manager through it, and completes the sign-in to the manager; `devctl auth status` shows the identities, never a token. Commands that need a token exit 8 naming `devctl auth login` when none is usable. See [docs/auth.md](docs/auth.md).

```bash
devctl auth login
devctl auth login --muster-only
devctl auth status
```

### Waiting for a pull request's CI (`devctl pr wait`)

`devctl pr wait <owner/repo> <number>` blocks until the pull request's head is green as the merge box sees it (the latest run per check, every CircleCI workflow of the head revision, no GitHub Actions run still open or awaiting approval, every required context reported), red, or in a state no CI can turn green (draft, closed, conflicting, behind a strict base), then prints one JSON document and exits 0, 1, 2 (timeout), 3, 4 (a required context never reported), 7 or 8. See [docs/pr-wait.md](docs/pr-wait.md).

```bash
devctl pr wait giantswarm/devctl 2277 --timeout 45m --progress
```

### Waiting for a release (`devctl release wait`)

`devctl release wait <owner/repo> (<vX.Y.Z> | --pr <n>)`, the wait `pr merge` runs after its merge, blocks until every image and chart of the tag is pullable and the tag pipeline is green (a repository's own tag jobs included), and prints one JSON document with the digests. The artifact names come from the sources that define them (the team-file entry for generated CI, the tag pipeline's push jobs for hand-written CI), never from the repository name; the public registry is probed anonymously, the private one with the docker keychain; a failed tag pipeline ends the wait as exit 1 with the failed jobs, a timeout as exit 2 naming what is missing. See [docs/release-wait.md](docs/release-wait.md) for the model, the JSON and the exit codes.

```bash
devctl release wait giantswarm/devctl v8.9.0
devctl release wait giantswarm/devctl --pr 2289 --progress
```

### Merging a pull request (`devctl pr merge`)

`devctl pr merge <owner/repo> <number>` is the one call an agent makes to land its own pull request: it runs the wait of `pr wait`, squash-merges the pull request (`--rebase` for a rebase merge) through the merge API with the judged head as the expected head, deletes the branch through the refs API, and then runs the wait of `release wait --pr` on the merge commit until the release is pullable (`--release-timeout`, 30 minutes by default; `--no-release-wait` ends at the merge). A base with a merge queue is enqueued and waited for instead of merged. A merge that no release follows (a repository that does not tag merge commits, an auto-release run that tagged nothing) is exit 0. Refused before any wait: a draft, closed or conflicting pull request and one behind a strict base (exit 3; `--update-branch` updates it and waits for the new head), another human's pull request and a repository whose team-file entry says `agentMerge: false` (exit 5). No protection setting is read to be changed or written: the merge is made as you, through the bypass the repository's ruleset grants its owning team and its admins. One JSON document (`pr wait`'s fields plus `mergeCommitSha`, `method`, `branchDeleted`, `enqueued` and `release`), exit 0, 1, 2, 3, 4, 5, 7 or 8, and after a merge 6 (the release failed) or 9 (the release not confirmed). See [docs/pr-merge.md](docs/pr-merge.md).

```bash
devctl pr merge giantswarm/devctl 2278 --timeout 45m --progress
```

### Repository set-up (`devctl repo`)

Giant Swarm repositories are declared in the team files of [giantswarm/github](https://github.com/giantswarm/github); the reconciler keeps them as declared. `devctl repo create` creates a repository as you, pushes its scaffold and opens the team-file pull request; the other verbs are giantswarm-repo-manager's tools called through muster as you (`devctl auth login --muster-only` first): `list`, `get`, `refresh`, `status`, `sweep`, `watch`, `adopt`, `update`, `transfer`, `set-lifecycle`, `approve`, `align`, each with `--dry-run` where it writes and `-o json` for the manager's answer. See [docs/repo.md](docs/repo.md).

```bash
devctl repo create --team bumblebee --name my-service --component-type service --flavour app --language go --description "What it does"
devctl repo watch my-service --pull-request 4711
devctl repo list --scope unassigned --inactive-days 365
devctl repo set-lifecycle old-tool archived --reason "replaced by new-tool" --dry-run
```

### Running Tests

```bash
make test
```

The suite includes the end-to-end scenarios under `e2e/`: the built binary against in-process mocks of GitHub,
CircleCI and the registry, one directory per known incident. See [e2e/README.md](e2e/README.md) for the format
and how to add one.

### Debug Mode

Set `LOG_LEVEL=debug` to see detailed output:

```bash
devctl --log-level debug repo status my-service
```

## Contributing

Please check our [contributing guidelines](CONTRIBUTING.md) for details on how to contribute to this project.

## License

devctl is licensed under the [Apache 2.0 License](LICENSE).
