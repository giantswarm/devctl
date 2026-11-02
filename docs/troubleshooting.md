# Troubleshooting

`devctl` tries to check if there is a newer version available before every command execution. If you happen to see this error:

```nohighlight
Error: GET https://api.github.com/repos/giantswarm/devctl/releases: 401 Bad credentials []
```

This means if you have probably either:

- The `GITHUB_TOKEN` environment variable is set to a token with not enough permissions.
- The `github.token` configuration option in `git` is set to a token with not enough permissions. You can verify that with `git config --get --null github.token`.

Workarounds:

- Run `unset GITHUB_TOKEN`.
- Run `git config --unset github.token`.
- Set `GITHUB_TOKEN` to a token with enough permissions.

## `make` stops with `VERSION is empty`

The generated `Makefile.gen.go.mk` takes the version from `gitsemver get`. gitsemver cannot read a repository whose git configuration sets `extensions.worktreeConfig` while `core.repositoryformatversion` is 0:

```nohighlight
error: resolving version for "HEAD": core.repositoryformatversion does not support extension: worktreeconfig
```

This fails in the main checkout and in every worktree of such a repository. Pass the version yourself: `make build VERSION=1.2.3`.
