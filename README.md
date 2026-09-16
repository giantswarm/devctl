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

### Repository set-up (`devctl repo`)

Giant Swarm repositories are declared in the team files of [giantswarm/github](https://github.com/giantswarm/github); the reconciler creates and keeps them as declared. `devctl repo create` validates a declaration through the engine, prints the dry run and opens the team-file pull request as you; `devctl repo status` prints a repository's set-up state. See [docs/repo.md](docs/repo.md).

```bash
devctl repo create --team bumblebee --name my-service --component-type service --flavour app --language go --description "What it does"
devctl repo status my-service
```

### Running Tests

```bash
go test ./...
```

### Debug Mode

Set `LOG_LEVEL=debug` to see detailed output:

```bash
devctl --log-level debug repo status my-service
```

## Contributing

Please check our [contributing guidelines](CONTRIBUTING.md) for details on how to contribute to this project.

## License

devctl is licensed under the [Apache 2.0 License](LICENSE).
