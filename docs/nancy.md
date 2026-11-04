# Local Nancy scan

From the root of a Go repository, run:

```sh
devctl nancy
```

Install Go and [Nancy v2.1.x](https://github.com/sonatype-nexus-community/nancy/releases/tag/v2.1.0)
on `PATH` first. The command lists the packages with `CGO_ENABLED=0 go list -json -deps ./...`, then
streams that list into `nancy sleuth --skip-update-check --db-cache-path "$HOME/.cache/nancy" --quiet --exclude-vulnerability-file
./.nancy-ignore --additional-exclude-vulnerability-files ./.nancy-ignore.generated`. These are the
dependency selection and scan options used by CircleCI's architect `go-test` scan (the generated
Makefile uses Nancy's default cache location instead). Run at the same commit, with the same ignore
files and Nancy version as CI to compare findings. The generated `make nancy` target remains
available to CI with its context credentials.

Before the first local scan, configure an OS keychain record using your keychain application's
secure editor. It uses devctl's existing keychain format:

- Service: `devctl` (Secret Service attribute `service` on Linux).
- Account: `ossindex` (Secret Service attribute `username` on Linux).
- Secret: a JSON object with `login` (your OSS Index username/email) and `token` (its API token).
  For example, the **shape** is `{"login":"<OSS Index username>","token":"<OSS Index API token>"}`.
  Optional `expiresAt` is an RFC 3339 timestamp; omit it for a token without an expiry.

Enter the real values only in the keychain editor. Keep this record out of repository files,
command arguments and shell history. Unlock the keychain before scanning. A missing, unreadable,
incomplete or expired record exits 8 with setup guidance. `devctl auth login` does not provision
this record: its GitHub, CircleCI and muster identities are separate.

The command reads the OS keychain directly, including on hosts where `DEVCTL_KEYRING_FILE` is set.
It hands the username and token only to Nancy's environment as `OSSI_USERNAME` and `OSSI_TOKEN`;
the `go list` process gets neither. Inherited scanner credentials are removed. An empty
`--guide-token=` flag prevents a Guide token in Nancy's own config from overriding this identity;
no secret is passed as an argument. The response cache uses `$HOME/.cache/nancy` as in CI;
endpoint environment overrides are retained as in CI. Other Nancy options use its defaults.

Nancy v2.1.x supports OSS Index credentials through the Sonatype Guide compatibility API and
warns that this mode is deprecated. Nancy v3 is outside this command's supported contract.
The scan requires access to that API (or Nancy's cached reports); it is not an offline vulnerability
database. Go may also need to fetch missing modules.

Nancy's stdout, stderr and positive exit status are preserved. A failed `go list` also fails the
command, even if Nancy returns success on its partial input. A scanner authentication failure,
outage or exhausted quota stays a local failure; CircleCI's separate outage soft-pass and JUnit
reporting are not reproduced. A zero local exit therefore requires both subprocesses to succeed.
