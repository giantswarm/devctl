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
| `auto-release` | `.github/workflows/zz_generated.auto_release.yaml` and `cliff.toml` (at repo root). Releases driven by conventional commits on `main` -- the workflow runs `git-cliff --unreleased --bump` on every push, computes the next semver, and creates the matching tag + GitHub Release atomically. No release PR, no human approval. `feat` bumps the minor version, a breaking change the major, every other type the patch; `docs` and `style` commits are skipped and release nothing. | Repos that want push-button releases from conventional commits. Requires `semantic_pull_request` enforcement on PR titles. |

Switching between values is bidirectional and self-cleaning: the chosen branch generates its own files and emits deletion inputs for the files of the other branch, so a flipped `--release-workflow` value over two consecutive gen runs leaves the repo with exactly one set of release files.

```nohighlight
devctl gen workflows --flavour app --language go --release-workflow=auto-release
```

#### Release candidates

A pull request title of `feat-rc:` or `fix-rc:` marks its change as belonging to a release candidate. The scope and the breaking marker are unchanged, so `feat-rc(auth)!: drop the v1 API` is valid. The workflow then decides over **every unreleased commit**, not just the ones in the push it handles:

> Tag a release candidate when at least one unreleased commit is `feat-rc` or `fix-rc`, and no unreleased `feat`, `fix` or breaking commit lacks the `-rc`.

`feat` and `fix` decide, everything else follows. A cycle is therefore sticky without any extra state:

| Merge | Tag | Why |
|-------|-----|-----|
| `feat-rc: add x` | `v1.3.0-rc.1` | one marked, none unmarked |
| `chore(deps): bump y` | `v1.3.0-rc.2` | `chore` does not decide, so a Renovate auto-merge cannot end a cycle |
| `fix: last thing` | `v1.3.0` | an unmarked `fix` closes the cycle |

The version does not drift while a cycle runs: the unreleased set still holds the original `feat`, so the closing commit lands on exactly the version the candidates were leading to. A candidate is flagged as a GitHub pre-release, so it does not surface as the repo's "Latest release", and the `/^v.*/` CircleCI tag filter publishes it like any other tag.

To close a cycle when the last candidate is good and no pull request is left to merge, run the workflow by hand with `release-type: stable`. `release-type: rc` forces one more candidate.

`feat-rc` and `fix-rc` are accepted as PR titles in every repo, because `semantic_pull_request` is generated for both release flows, but they only act under `auto-release`. In a `legacy` repo they are inert.

`cliff.toml`'s `[remote.github].repo` is auto-detected from the consuming repo's `origin` git remote URL. Run from a directory whose `git config remote.origin.url` points at `github.com/giantswarm/<repo>`; outside a git repo the value renders as `""` and git-cliff's GitHub API lookups fail at workflow runtime.

### helm-docs regen workflow

`--helm-docs-regen` (app flavour only, off by default) adds `.github/workflows/zz_generated.helm-docs-regen.yaml`. On pull requests from `renovate/**` and `dependabot/**` branches that touch `helm/**` or `.pre-commit-config.yaml`, the workflow regenerates the files the generated pre-commit hooks derive from a chart's `values.yaml` -- the helm-docs README and `values.schema.json` -- and pushes the result back onto the PR branch. Without it, every dependency bump that changes an image tag or a values key fails the `pre-commit` check on files only the hooks can rewrite ("files were modified by this hook"), and the PR waits for a human to run the hooks and push. The Mend-hosted Renovate app cannot run `postUpgradeTasks`, so the regeneration has to happen in the consuming repo.

How it works:

- A `preflight` job reads the repo's own `.pre-commit-config.yaml`: every `helm-schema-<chart>` hook plus the hooks of the `norwoodj/helm-docs` entry, whose `rev` names the helm-docs release to install. Nothing in the workflow is repo-specific, so it needs no regeneration when a chart is added or a tool is bumped. No hooks (or no `TAYLORBOT_GITHUB_ACTION` secret, as on fork and Dependabot-triggered runs, which only see Dependabot secrets) skips the second job with a warning.
- A `regen` job checks out the head branch with `secrets.TAYLORBOT_GITHUB_ACTION`, installs that helm-docs release (the schema hook installs and pins its own generator through `additional_dependencies`), and runs each hook twice through pre-commit: the first pass may rewrite files, the second pass has to be clean. A hook that still rewrites, or errors out, fails the job and nothing is pushed.
- A changed tree is committed as `taylorbot <dev@giantswarm.io>` and pushed. The push uses the PAT because a `GITHUB_TOKEN` push does not re-trigger the required checks on the new commit. A clean tree is a no-op, so the run the push triggers on the new head exits without pushing again. Runs are serialised per head ref with `cancel-in-progress`.
- `devctl gen renovate` lists `dev@giantswarm.io` in `gitIgnoredAuthors`, so Renovate treats the regen commit as its own and keeps rebasing and autoclosing the PR. A repo whose `renovate-custom.json5` sets its own `gitIgnoredAuthors` must repeat the address: Renovate replaces the array instead of merging it.

In giantswarm/github the flag is `gen.helmDocsRegen: true` on the repository entry.

```nohighlight
devctl gen workflows --flavour app --language generic --helm-docs-regen
```

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
