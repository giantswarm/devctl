[![CircleCI](https://dl.circleci.com/status-badge/img/gh/giantswarm/devctl/tree/main.svg?style=svg)](https://dl.circleci.com/status-badge/redirect/gh/giantswarm/devctl/tree/main)

# devctl

Command line tool for the daily development business at Giant Swarm.

## Quick start

### Installation

Install the [latest release](https://github.com/giantswarm/devctl/releases/latest). Please do not use `go install`.

### Configuration

Most commands require credentials for GitHub write access to be available. Make sure you have the environment variable

```nohighlight
GITHUB_TOKEN
```

set with a valid [personal access token](https://github.com/settings/tokens) as the value.

### Usage

Please check `devctl --help` for available commands and options.

Also see the [docs](docs/) folder for more details on some commands.

### Updating

```nohighlight
devctl version update
```

### Development

While running locally during development you may get some errors relating to `no matching files found` for some of the templates. If you do run `make generate-go` to generate these template files before running.
