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
