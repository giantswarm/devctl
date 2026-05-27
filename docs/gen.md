# Using the `gen` commands to generate files

The `gen` command family is designed to create common files in repositories, adapted specifically for the repository [flavour](flavours.md) and/or programming language used.

Files are written to the current directory. The assumption is that the current working directory is the root directory of a cloned repository.

Usually these commands are executed via automation in the [giantswarm/github](https://github.com/giantswarm/github/actions/workflows/synchronize.yaml) repository, but this can also be done manually/locally.

Note: the added files are not meant for later editing, as changes would be overwritten by a subsequent `devctl` execution.

## Generating workflow files

Creates common GitHub actions workflows (for CI/CD) in the `.github/workflows` directory.

Example:

```nohighlight
devctl gen workflows --flavour cli
```

### Release Please

To opt a repository into [Release Please](https://github.com/googleapis/release-please) instead of the legacy release workflow:

```nohighlight
devctl gen workflows --flavour app --language go \
  --release-workflow release-please \
  --changelog-style legacy \
  --auto-release-level minor
```

| Flag | Values | Default | Notes |
|------|--------|---------|-------|
| `--release-workflow` | `legacy`, `release-please` | `legacy` | Switches between the legacy `create-release-pr` flow and Release Please |
| `--changelog-style` | `legacy`, `release-please` | `legacy` | `legacy` maps commit types to `### Added/Changed/Fixed` (required by the `giantswarm/releases` changelog scraper). `release-please` uses the Angular preset (`### Features`, `### Bug Fixes`, etc.) |
| `--auto-release-level` | `none`, `patch`, `minor`, `major` | `none` | Auto-merges the Release Please PR when CI passes, up to this bump level (sets the reusable workflow's `auto-merge-level` input). `none` disables auto-merge. Requires "Allow auto-merge" enabled on the repo and the `release-please` GitHub App on its branch-protection bypass list. |

In `release-please` mode, three files are written:

- `.github/workflows/zz_generated.release-please.yaml` — regenerated on every `devctl gen` run
- `release-please-config.json` — written once; edit freely to add `version-files` or other Release Please settings
- `.release-please-manifest.json` — written once; updated by Release Please on every run to track the current version

## Generating Makefiles

Creates common `Makefile` and includes in the root directory.

Example:

```nohighlight
devctl gen workflows --flavour cli --language go
```

## Generating pre-commit configuration

Creates a `.pre-commit-config.yaml` file in the repo root with hooks appropriate for the repository's language and content.

The `--language` flag sets the primary language (`go`, `python`, `generic`). The `--flavors` flag enables additional hook groups:

- `bash` — shell script linting via `pre-commit-shell`
- `md` — Markdown linting via `markdownlint-cli`
- `helmchart` — Helm chart schema and docs hooks (auto-detects charts under `helm/`)

Examples:

```nohighlight
devctl gen precommit --language go --repo-name devctl
devctl gen precommit --language go --repo-name my-app --flavors bash,helmchart
devctl gen precommit --language generic --repo-name my-service --flavors md,bash
```

## Generating renovate configuration

Generates a `renovate.json5` file in the repo root to configure [renovate](https://docs.renovatebot.com/), which automatically updates dependencies in the configured repository.

```nohighlight
devctl gen renovate --language LANGUAGE
```

Note: The `LANGUAGE` value is not validated currently. From code, as of writing this docs, `go` and `python` were the only values checked for. (Usability improvement welcome!)
