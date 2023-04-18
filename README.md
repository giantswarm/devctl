[![CircleCI](https://circleci.com/gh/giantswarm/devctl.svg?style=shield&circle-token=5f432129bee4f3b1d8a875c5c2bf8aed0cda6bea)](https://circleci.com/gh/giantswarm/devctl)

# devctl

Command line tool for the development daily business.

## Installation

This project uses Go modules. Be sure to have it outside your `$GOPATH` or
set `GO111MODULE=on` environment variable. Then regular `go install` should do
the trick. Alternatively the following one-liner may help.

```sh
GO111MODULE=on go install -ldflags "-X 'github.com/giantswarm/devctl/pkg/project.gitSHA=$(git rev-parse HEAD)'" .
```

## Configuration

To be able to fully use `devctl` you need to set following environment variables.

- `DEVCTL_GITHUB_ACCESS_TOKEN`: GitHub access token generated with your
  personal account.

## Usage

For full capabilities, please check `devctl -h` for details on all functions.

### Generating files

This command is mostly used to distribute common files across multiple repositories, for example:

- GitHub workflow via: `devctl gen workflows --flavour ...`
- Makefiles: `devctl gen makefile --flavour ...`
- Makefiles: `devctl gen renovate --language ...`
- Makefiles: `devctl gen dependabot --ecosystems ...`

These are distributed via https://github.com/giantswarm/github and configuration passed to the commands are kept
within https://github.com/giantswarm/github/tree/master/repositories.

### Generating releases

The tool can be used to create legacy cluster releases, for example the ones stored in https://github.com/giantswarm/releases.

```shell
devctl release create --provider aws --base 18.0.1 --name 18.0.2 --component aws-operator@13.2.1-dev --overwrite
```

### Updating the tool

There is a command that can be used to self-upgrade the tool.

```shell
devctl version update
```

## Troubleshooting

`devctl` tries to check if there is a newer version available before every command execution. If you happen to see this error:

```
Error: GET https://api.github.com/repos/giantswarm/devctl/releases: 401 Bad credentials []
```

This means if you have probably either:

- `GITHUB_TOKEN` variable set to a token with not enough permissions.
- `github.token` configuration set to a token with not enough permissions. You can verify that with `git config --get --null github.token`.

Workarounds:

- Run `unset GITHUB_TOKEN`.
- Run `git config --unset github.token`.
- Set `GITHUB_TOKEN` to a token with enough permissions.
