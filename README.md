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

`devctl auth login` logs in to GitHub (the device flow of the devctl GitHub App, refreshed without a human) and CircleCI (OAuth 2.0 with PKCE and dynamic client registration, a 90-day token) and keeps both tokens in the OS keychain; `devctl auth status` shows the identities, never a token. Commands that need a token exit 8 naming `devctl auth login` when none is usable. See [docs/auth.md](docs/auth.md).

```bash
devctl auth login
devctl auth status
```

### Waiting for a pull request's CI (`devctl pr wait`)

`devctl pr wait <owner/repo> <number>` blocks until the pull request's head is green as the merge box sees it (the latest run per check, every CircleCI workflow of the head revision, no GitHub Actions run still open or awaiting approval, every required context reported), red, or in a state no CI can turn green (draft, closed, conflicting, behind a strict base), then prints one JSON document and exits 0, 1, 2 (timeout), 3, 4 (a required context never reported), 7 or 8. See [docs/pr-wait.md](docs/pr-wait.md).

```bash
devctl pr wait giantswarm/devctl 2277 --timeout 45m --progress
```

### Waiting for a release (`devctl release wait`)

`devctl release wait <owner/repo> (<vX.Y.Z> | --pr <n>)` blocks until every image and chart of the tag is pullable and prints one JSON document with the digests. The artifact names come from the sources that define them (the team-file entry for generated CI, the tag pipeline's push jobs for hand-written CI), never from the repository name; the public registry is probed anonymously, the private one with the docker keychain; a failed tag pipeline ends the wait as exit 1 with the failed jobs, a timeout as exit 2 naming what is missing. See [docs/release-wait.md](docs/release-wait.md) for the model, the JSON and the exit codes.

```bash
devctl release wait giantswarm/devctl v8.9.0
devctl release wait giantswarm/devctl --pr 2289 --progress
```

### Repository set-up (`devctl repo`)

Giant Swarm repositories are declared in the team files of [giantswarm/github](https://github.com/giantswarm/github); the reconciler creates and keeps them as declared. `devctl repo create` validates a declaration through the engine, prints the dry run and opens the team-file pull request as you; `devctl repo status` prints a repository's set-up state. See [docs/repo.md](docs/repo.md).

```bash
devctl repo create --team bumblebee --name my-service --component-type service --flavour app --language go --description "What it does"
devctl repo status my-service
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
