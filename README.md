[![CircleCI](https://circleci.com/gh/giantswarm/devctl.svg?style=shield&circle-token=5f432129bee4f3b1d8a875c5c2bf8aed0cda6bea)](https://circleci.com/gh/giantswarm/devctl)

# devctl

Command line tool for the development daily business.

## Installation

This project uses Go modules. Be sure to have it outside your `$GOPATH` or
set `GO111MODULE=on` environment variable. Then regular `go install` should do
the trick. Alternatively the following one-liner may help. 

```sh
GO111MODULE=on go install -ldflags "-X main.gitCommit=$(git rev-parse HEAD)" .
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
