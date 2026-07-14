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

### Release workflow

`--release-workflow` selects which release flow to generate. Two values:

| Value | What's emitted | When to use |
|-------|----------------|-------------|
| `legacy` (default) | `.github/workflows/zz_generated.create_release.yaml`, `zz_generated.create_release_pr.yaml`, `zz_generated.validate_changelog.yaml`. Releases driven by manually-pushed `main#release#patch`-style branches that open a release PR for human approval. | The historical flow; in use by most giantswarm repos today. |
| `auto-release` | `.github/workflows/auto-release.yaml` and `cliff.toml` (at repo root). Releases driven by conventional commits on `main` -- the workflow runs `git-cliff --unreleased --bump` on every push, computes the next semver, and creates the matching tag + GitHub Release atomically. No release PR, no human approval. | Repos that want push-button releases from conventional commits. Requires `semantic_pull_request` enforcement on PR titles. |

Switching between values is bidirectional and self-cleaning: the chosen branch generates its own files and emits deletion inputs for the files of the other branch, so a flipped `--release-workflow` value over two consecutive gen runs leaves the repo with exactly one set of release files.

```nohighlight
devctl gen workflows --flavour app --language go --release-workflow=auto-release
```

`cliff.toml`'s `[remote.github].repo` is auto-detected from the consuming repo's `origin` git remote URL. Run from a directory whose `git config remote.origin.url` points at `github.com/giantswarm/<repo>`; outside a git repo the value renders as `""` and git-cliff's GitHub API lookups fail at workflow runtime.

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
