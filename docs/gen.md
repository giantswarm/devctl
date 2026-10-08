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
| `auto-release` | `.github/workflows/zz_generated.auto_release.yaml` and `cliff.toml` (at repo root). Releases driven by conventional commits on `main` -- the workflow runs `git-cliff --unreleased --bump` on every push, computes the next semver, and creates the next release candidate tag + GitHub pre-release atomically. A stable release is a manual promotion of the latest candidate. `feat` bumps the minor version, a breaking change the major, every other type the patch; `docs`, `style` and `test` commits are skipped and release nothing. | Repos that want push-button releases from conventional commits. Requires `semantic_pull_request` enforcement on PR titles. |

Switching between values is bidirectional and self-cleaning: the chosen branch generates its own files and emits deletion inputs for the files of the other branch, so a flipped `--release-workflow` value over two consecutive gen runs leaves the repo with exactly one set of release files.

A fork line (`--flavour fork`) gets nothing generated on `legacy`. On `auto-release` it gets the release flow alone: the workflow, `cliff.toml` and the PR title check. `--release-branch` names the branch the line is consumed from (the declaration's `defaultBranch`, e.g. `giantswarm`), and pushes to that branch alone cut releases. A re-pin rebases that branch onto a new upstream commit, which leaves the line's earlier tags unreachable, while the upstream tags the mirror copies stay reachable. So the line counts from its highest stable tag rather than the nearest reachable one, and git-cliff counts only the tags of that tag's major. After a re-pin, the workflow moves that tag onto the commit the branch still shares with it, in its own checkout only, and the next candidate carries what changed since.

```nohighlight
devctl gen workflows --flavour app --language go --release-workflow=auto-release
```

#### Release candidates and stable releases

Every releasable push cuts the next release candidate, `vX.Y.Z-rc.N`, for the version git-cliff computes from the conventional commits since the last stable release:

| Merge | Tag | Why |
|-------|-----|-----|
| `feat: add x` | `v1.3.0-rc.1` | a `feat` puts the cycle on the next minor |
| `chore(deps): bump y` | `v1.3.0-rc.2` | releasable, so the next candidate |
| `docs: fix a typo` | none | `docs` is skipped, so there is nothing new to put in a candidate |
| `refactor!: drop y` | `v2.0.0-rc.1` | a breaking change moves the target, and the series restarts at `rc.1` |

A push whose commits git-cliff all skips (`docs`, `style`, `test`) or drops as non-conventional tags nothing, so a README typo cannot spend an rc number and a publish pipeline. A candidate is flagged as a GitHub pre-release, so it does not surface as the repo's "Latest release", and the `/^v.*/` CircleCI tag filter publishes it like any other tag.

A stable release is cut only by running the workflow by hand with `release-type: stable`. It promotes the latest candidate since the last stable release: the workflow checks that candidate out, so the version, the release notes and the tag's commit are all the candidate's, and whatever merged after it waits for the next candidate. The run fails when there is no candidate to promote, when the candidate has no GitHub pre-release (the run that cut it did not finish), or when a commit status of its commit failed or is still pending (the pipelines that build and publish it report there). Only commit statuses count, which CircleCI writes; a GitHub Actions workflow of the repository's own that publishes on a tag reports check runs, and the promotion does not wait for it. `release-type: auto` applies the same rule as a push. A `stable` run queued behind a push run waits in the branch's concurrency group, where GitHub keeps one waiting run: a further push replaces it, and the promotion has to be started again.

`cliff.toml`'s `[remote.github].repo` is read from the consuming repo's `origin` git remote URL, its `giantswarm/<repo>` path. A checkout with no origin remote, or one outside `giantswarm`, needs `--repo-name <repo>` instead; without either the command fails rather than silently writing `repo = ""`.

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

### README link check workflow

The app flavour generates `.github/workflows/zz_generated.check_readme_links.yaml`, which calls the `check-readme-links` reusable workflow of giantswarm/github-workflows. A pull request that touches `README.md` fails on a dead URL, a relative path that does not exist in the repository or a reversed link `(text)[url]`. A weekly run catches links whose target moved without the README changing, which no pull request trigger sees, and files its findings as one issue, `Broken links in README`, updated on later runs and closed with a comment once the links are fixed. The issue is filed, updated and closed from the default branch only: a `workflow_dispatch` on another branch keeps its report in the job summary and the run's artifact. The ignore list and the checker versions live in giantswarm/github-workflows under `link-check/` and are taken from `main` whatever ref the generated workflow pins, so a fix there reaches every repository without a regeneration.

`--check-readme-links=false` (app flavour only, on by default) keeps the workflow out of a repository. In giantswarm/github that is `gen.checkReadmeLinks: false` on the repository entry; without it align-files puts the workflow back on every run.

```nohighlight
devctl gen workflows --flavour app --language generic --check-readme-links=false
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

No hook reads protobuf-generated code: the config's top-level `exclude` skips `*_pb.*` (protoc-gen-es, grpc-tools), `*_pb2.py`, `*_pb2.pyi`, `*_pb2_grpc.py` and `*.pb.go` with its `_grpc`, `.gw`, `.validate` and `_vtproto` variants. Their generator is the source of truth, so a hook that rewrote them (`end-of-file-fixer` on buf's TypeScript output, `go-imports -local`, `ruff --fix`) would be undone by the next generation. Hand-written files next to them, a `gen/` directory's `README.md` or `buf.gen.yaml` included, are still checked. The fixer hooks also skip `testdata/`, `.yarn/` and the vendored subcharts under `helm/<chart>/charts/`.

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

Note: The `LANGUAGE` value is not validated currently. `go`, `python` and `node` add the matching language preset; every other value (e.g. `generic`) renders the base preset alone.

The giantswarm/github align-files workflow runs this generator for repositories on devctl-generated CI (with `--circleci-generated`, which disables Renovate's architect-orb updates and extends the ATS preset because `gen circleci` bakes those in) and for the [`customer` flavour](flavours.md#customer) (without it: customer repositories have no CircleCI). Every other repository keeps its hand-maintained `renovate.json5`.

## Generating CircleCI configuration

Generates the dynamic-config pipeline of a repository on devctl-generated CI: `.circleci/config.yml` (the static setup workflow, which merges the optional repo-owned `.circleci/custom.yml` in at pipeline runtime) and `.circleci/workflows.yml` (the pipeline), plus the canonical `tests/ats` dependency file of a chart repository. The chart tests themselves -- `.ats/main.yaml` and the Python files under `tests/ats` -- are the repository's own and never generated; app-test-suite picks the pytest executor from the dependency file and refuses a `tests/ats` without a test, so a chart repository carries at least one (`devctl repo create` writes a first smoke test into a new repository's scaffold).

```nohighlight
devctl gen circleci --repo-name REPOSITORY --language LANGUAGE --flavour FLAVOUR[,FLAVOUR]
```

The pipeline is derived from the repository's signals — the language, the flavours, the presence of a `Dockerfile`, a `.ats/kind-config.yaml`, a lockfile or `.nvmrc` — rather than configured: `go` and `node` select a build/test job, a `Dockerfile` the image jobs, the `app` flavour the chart jobs (build, chart tests, push on the release tag). The knobs the giantswarm/github team file exposes under `gen.ci` are the flags of this command; `devctl gen circleci --help` documents each, and the architect-orb version is baked into devctl next to the template.

### Template repositories

A repository other repositories are created from (`componentType: template` in the team file, e.g. `giantswarm/template-app`) carries a chart at `helm/{APP-NAME}` with the placeholders the set-up engine fills in — `{APP-NAME}`, `{TEAM-NAME}` (the chart's `io.giantswarm.application.team` label) and `{APP HELM REPOSITORY}` — so app-build-suite cannot build it as it is. With `--component-type template --team TEAM` the chart job renders the checkout first and builds the rendered chart:

```nohighlight
devctl gen circleci --repo-name template-app --language generic --flavour app --component-type template --team team-honeybadger
```

The app name (`sample-app`) and the Helm repository (`https://charts.example.com`) are fixtures; the team is the owning team from the team file (`team-honeybadger` and `honeybadger` both name it), because the team label is what the first chart build of a created repository is validated on. Green means a repository created from the template passes that first build. Nothing is released from a template, so the pipeline has no chart-test job, no push jobs and no release leg, the build runs on every branch including `main`, and no `tests/ats` files are written. A template without a chart (no `app` flavour) is unchanged by the flag; every other `--component-type` value is inert.
