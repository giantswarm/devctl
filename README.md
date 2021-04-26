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

- `DEVCTL_GITHUB_ACCESS_TOKEN`: Github access token generated with your
  personal account.

## Usage

Please check `devctl -h` for for details on all functions.

### Find repositories depending on a Go package/module

For example, to find all our repositories using `github.com/giantswarm/microerror`:

```
devctl repo list --depends-on github.com/giantswarm/microerror
```

## Troubleshooting

`devctl` tries to check if there is a newer version available before every command execution. If you happen to see this error:

```
Error: GET https://api.github.com/repos/giantswarm/devctl/releases: 401 Bad credentials []
```

This means if you have probably either:

- `GITHUB_TOKEN` variable set to a token with not enought permissions.
- `github.token` configuration set to a token with not enough permissions. You can verify that with `git config --get --null github.token`.

Workarounds:

- Run `unset GITHUB_TOKEN`.
- Run `git config --unset github.token`.
- Set `GITHUB_TOKEN` to a token with enough permissions.
