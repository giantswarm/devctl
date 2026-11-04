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

The generated `Makefile.gen.go.mk` first takes the version from `gitsemver get`. Older gitsemver releases cannot read a linked worktree's `.git` file, or a repository with `extensions.worktreeConfig` set:

```nohighlight
error: resolving version for "HEAD": core.repositoryformatversion does not support extension: worktreeconfig
```

Regenerate the Makefile with `devctl gen makefile --language go` (keeping the repository's usual flavour flags).
If gitsemver fails or returns no version, the generated lookup now uses local tags with
`git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*'`, stripping the leading `v`. At `v1.2.3` it stamps `1.2.3`;
after that tag it stamps `1.2.3-<distance>-g<commit>`. No fetch or tool installation runs during lookup.

A recent gitsemver supports worktrees directly and preserves its usual development-version format.
If neither lookup finds a version (for example, an older gitsemver in a worktree with no reachable version tag),
builds and installs still stop. Supply the version explicitly: `make build VERSION=1.2.3`.
