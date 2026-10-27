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

A repository's release workflow is its declaration in giantswarm/github (`gen.ci.releaseWorkflow` in its team-file entry), which align-files passes as `--release-workflow`. Auto Release files added to a repository whose declaration says `legacy` are deleted again by the next run, so both places name the declaration as the place to change instead: a `legacy` run that deletes `zz_generated.auto_release.yaml` or `cliff.toml` warns on stderr, and the generated `zz_generated.validate_changelog.yaml` fails a pull request whose head carries either file (job "Release workflow matches the declaration").

A fork line (`--flavour fork`) gets nothing generated on `legacy`. On `auto-release` it gets the release flow alone: the workflow, `cliff.toml` and the PR title check. `--release-branch` names the branch the line is consumed from (the declaration's `defaultBranch`, e.g. `giantswarm`), and pushes to that branch alone cut releases. A re-pin rebases that branch onto a new upstream commit, which leaves the line's earlier tags unreachable, while the upstream tags the mirror copies stay reachable. So the line counts from its highest stable tag rather than the nearest reachable one, and git-cliff counts only the tags of that tag's major. After a re-pin, the workflow moves that tag onto the commit the branch still shares with it, in its own checkout only, and the next candidate carries what changed since. `--maintenance-branches` (team file `gen.ci.maintenanceBranches: true`) adds the line's maintenance branches `release-X.Y`: each releases patches of its own X.Y series, counted from that series' highest stable tag, so a backported feature is a patch and the next minor stays the consumed branch's. A maintenance branch carries the same rendering as the consumed branch.

```nohighlight
devctl gen workflows --flavour app --language go --release-workflow=auto-release
```

#### Release candidates and stable releases

Every releasable push releases the version git-cliff computes from the conventional commits since the last stable release. A minor or major version is cut as the next release candidate, `vX.Y.0-rc.N`; a patch of the last stable release (only fixes and chores since it) is the fast path and is tagged `vX.Y.Z` stable on its push, so there is never a patch candidate:

| Merge (after `v1.2.9`) | Tag | Why |
|-------|-----|-----|
| `fix: x` | `v1.2.10` | a patch of the last stable release, tagged stable on its push |
| `feat: add x` | `v1.3.0-rc.1` | a `feat` puts the cycle on the next minor |
| `fix: y` | `v1.3.0-rc.2` | a fix during a cycle rides its next candidate |
| `chore(deps): bump y` | `v1.3.0-rc.3` | releasable, so the next candidate |
| `docs: fix a typo` | none | `docs` is skipped, so there is nothing new to put in a candidate |
| `refactor!: drop y` | `v2.0.0-rc.1` | a breaking change moves the target, and the series restarts at `rc.1` |

The first release of a repository with no stable tag is a candidate. On a maintenance branch (`release-v3.x`, a fork line's `release-X.Y`) every release is a patch of its series, so it is tagged stable on its push. A patch candidate cut before this rule is released by the next releasable push as its stable version.

`--patch-candidates` (team file `gen.ci.patchCandidates: true`) drops the fast path: a patch is cut as a candidate too, `v1.2.10-rc.1` for `fix: x` above and `v1.2.10-rc.2` for the next fix, on the default branch and on a maintenance branch alike, so no push tags a stable release. It is for a repository whose consumers follow a semver range that takes its next patch on their own (`>=1.17.0 <1.18.0`): every stable release is then a promotion of a candidate proven first. A maintenance branch's candidate is promoted on that branch, `devctl release promote <owner/repo> --branch release-X.Y --version vX.Y.Z-rc.N`.

A push whose commits git-cliff all skips (`docs`, `style`, `test`) or drops as non-conventional tags nothing, so a README typo cannot spend an rc number and a publish pipeline. A candidate is flagged as a GitHub pre-release, so it does not surface as the repo's "Latest release", and the `/^v.*/` CircleCI tag filter publishes it like any other tag.

A job of the repository's own that must not see a candidate, in a hand-written pipeline or in `.circleci/custom.yml`, filters on stable tags before the repository moves to `auto-release`. The krew index update of a CLI is one: krew-release-bot refuses a GitHub pre-release, so a `/^v.*/` filter turns the first candidate's pipeline red.

```yaml
workflows:
  release:
    jobs:
      - update-krew-index:
          filters:
            branches:
              ignore: /.*/
            tags:
              only: /^v\d+\.\d+\.\d+$/
```

A stable minor or major release is cut only by running the workflow by hand with `release-type: stable`. It promotes the latest candidate since the last stable release, or the one its `version` input names (`vX.Y.Z-rc.N`, one of those candidates: one built while a later one still builds, or the one proven; any other tag fails the run): the workflow checks that candidate out, so the version, the release notes and the tag's commit are all the candidate's, and whatever merged after it waits for the next candidate. The run fails when there is no candidate to promote, when the candidate has no GitHub pre-release (the run that cut it did not finish), or when a commit status of its commit failed or is still pending (the pipelines that build and publish it report there). A failed `ci/circleci: sync-china-registry`, the in-China mirror the release does not require, is a warning on the run, not a refusal, as in `devctl release wait` and `devctl release promote`. Only commit statuses count, which CircleCI writes; a GitHub Actions workflow of the repository's own that publishes on a tag reports check runs, and the promotion does not wait for it. A repository's own rules for a stable release go in `.github/release-gate.custom.sh`: when the released commit carries it (the promoted candidate, or a patch push's head), the run executes it with bash on that checkout, with the version about to be tagged as its argument, after the version is decided and before the tag and the GitHub release are created; a non-zero exit fails the run and cuts neither. A repository without the script has no gate. A check in the tag's own pipeline runs after the release exists, so a refusal there leaves a release that looks published with nothing built. One commit releases one version: every run that is about to tag, a candidate or a stable, first reads the remote's tags, and when the commit already carries the release tag (`vX.Y.Z`, `vX.Y.Z-rc.N`) of another version the run fails naming both and cuts no tag and no release. A stable on its own candidate's commit is the same version and passes; a tag outside that pattern (an upstream pre-release) counts for nothing. `release-type: auto` applies the same rule as a push. A `stable` run queued behind a push run waits in the branch's concurrency group, where GitHub keeps one waiting run: a further push replaces it, and the promotion has to be started again.

`cliff.toml`'s `[remote.github].repo` is read from the consuming repo's `origin` git remote URL, its `giantswarm/<repo>` path. A checkout with no origin remote, or one outside `giantswarm`, needs `--repo-name <repo>` instead; without either the command fails rather than silently writing `repo = ""`.

### helm-docs regen workflow

`--helm-docs-regen` (app flavour only, off by default) adds `.github/workflows/zz_generated.helm-docs-regen.yaml`. On pull requests from `renovate/**` and `dependabot/**` branches that touch `helm/**` or `.pre-commit-config.yaml`, the workflow regenerates the files the generated pre-commit hooks derive from a chart's `values.yaml` -- the helm-docs README and `values.schema.json` -- and pushes the result back onto the PR branch. Without it, every dependency bump that changes an image tag or a values key fails the `pre-commit` check on files only the hooks can rewrite ("files were modified by this hook"), and the PR waits for a human to run the hooks and push. The Mend-hosted Renovate app cannot run `postUpgradeTasks`, so the regeneration has to happen in the consuming repo.

How it works:

- A `preflight` job reads the repo's own `.pre-commit-config.yaml`: every `helm-schema-<chart>` hook plus the hooks of the `norwoodj/helm-docs` entry, whose `rev` names the helm-docs release to install. Nothing in the workflow is repo-specific, so it needs no regeneration when a chart is added or a tool is bumped. No hooks (or no `RELEASE_BOT_CLIENT_ID`/`RELEASE_BOT_PRIVATE_KEY` secrets, as on fork and Dependabot-triggered runs, which only see Dependabot secrets) skips the second job with a warning.
- A `regen` job mints a short-lived token of the `giantswarm-release` GitHub App (`secrets.RELEASE_BOT_CLIENT_ID`, `secrets.RELEASE_BOT_PRIVATE_KEY`), checks out the head branch with it, installs that helm-docs release (the schema hook installs and pins its own generator through `additional_dependencies`), and runs each hook twice through pre-commit: the first pass may rewrite files, the second pass has to be clean. A hook that still rewrites, or errors out, fails the job and nothing is pushed.
- A changed tree is committed as `giantswarm-release[bot]` and pushed. The push uses the App token because a `GITHUB_TOKEN` push does not re-trigger the required checks on the new commit. A clean tree is a no-op, so the run the push triggers on the new head exits without pushing again. Runs are serialised per head ref with `cancel-in-progress`.
- `devctl gen renovate` lists the regen commit's author `342014845+giantswarm-release[bot]@users.noreply.github.com` (and taylorbot's `dev@giantswarm.io` for repositories not regenerated yet) in `gitIgnoredAuthors`, so Renovate treats the regen commit as its own and keeps rebasing and autoclosing the PR. A repo whose `renovate-custom.json5` sets its own `gitIgnoredAuthors` must repeat the addresses: Renovate replaces the array instead of merging it. If the App token cannot be minted (the App is not installed on the repository, or its key is stale), the regen job fails loudly instead of skipping: that is a broken set-up to fix, not a missing secret.

In giantswarm/github the flag is `gen.helmDocsRegen: true` on the repository entry.

```nohighlight
devctl gen workflows --flavour app --language generic --helm-docs-regen
```

### Reservation command workflow

`--reservation-command` (app flavour only, off by default) adds `.github/workflows/zz_generated.reservation_command.yaml`, a self-contained workflow with one job. A pull request comment that starts a line with `/deploy`, `/undeploy` or `/extend`, and a closed pull request, are sent to giantswarm/mc-reservations as a `repository_dispatch`, which reserves a management cluster for the branch's dev build (see `devctl reservation`). The workflow is a pure trigger: it sends only the repository, the pull request number and the comment ID, and mc-reservations re-reads and checks the rest. A job filter skips other comments, bot comments and the close of a fork's pull request, so they start no run. The repository needs the `MC_RESERVATIONS_DISPATCH_CLIENT_ID` and `MC_RESERVATIONS_DISPATCH_APP_KEY` org secrets, and the `mc-reservations-github` App installed, or a command gets no answer.

In giantswarm/github the flag is `gen.reservationCommand: true` on the repository entry.

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

A Go repository's config also refuses a `require` line in any `go.mod` (single or in a block) whose trailing comment has more than one word (`go-mod-require-comment`): Renovate's gomod manager reads only a one-word comment such as `// indirect` and skips the whole line otherwise, so that dependency is never updated and nothing says so. The explanation goes on a comment line above the require.

A Go repository's `golangci-lint` hook, like `make lint` of `devctl gen makefile --language go`, passes no linter flags: the linters live in the repository's golangci-lint configuration, so a bare `golangci-lint run` lints with the same set. Both commands write `.golangci.yaml` (gosec and goconst on top of the standard set) when the repository has no configuration. A repository's own `.golangci.yml` or `.golangci.yaml` is kept as it is, except that gosec and goconst are added to `linters.enable` when it neither enables nor disables them (and govet too when `linters.default` replaces the standard set); a TOML or JSON configuration, or one whose `linters.enable` is a flow sequence, fails generation naming the linters to enable.

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

A GitHub fork takes `--flavour fork`. Renovate skips a fork ("Repository is a fork and not manually configured - skipping") unless its default config file, `renovate.json` and never `renovate.json5`, sets `forkProcessing` to `"enabled"` itself; a preset cannot set it. The fork flavour therefore writes the same config as JSONC (comments kept, otherwise strict JSON, which is how Renovate parses `renovate.json`) into `renovate.json` with that key, and removes `renovate.json5`; it leaves `.github/dependabot.yml`, which on a fork line is upstream's, where the other flavours remove it. The fork line's own rules (`enabledManagers`, `customManagers`, `dependencyDashboard`) go into `renovate-custom.json5` as on any other repository. A fork line whose config turns Renovate off (`enabled: false`) keeps it: the command refuses to replace the opt-out, and Renovate skips the fork anyway. align-files generates nothing for a fork line, so the command runs by hand.

```nohighlight
devctl gen renovate --language go --flavour fork
```

## Generating a plans repository's set-up

Writes the set-up every team plans repository shares. Nothing is generated without the [`plans` flavour](flavours.md#plans).

```nohighlight
devctl gen plans --flavour plans
```

`.claude/settings.json` enables the `gs-plans` plugin, the plan-workflow skills, from the `giantswarm` marketplace (giantswarm/claude-code), with `autoUpdate` on so Claude Code refreshes the marketplace and its plugins after startup. devctl owns two entries, `extraKnownMarketplaces.giantswarm` and `enabledPlugins["gs-plans@giantswarm"]`, and keeps every other key of an existing file, so a team adds its own settings in the same file. The JSON is rewritten with sorted keys and two-space indentation; strings and numbers stay as written. The skills live in the plugin, not in the repository; the repository's own rules, such as which plan stages are required, stay in its `AGENTS.md`.

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

### Multi-image repositories

A repository of the [`multi-image` flavour](flavours.md#multi-image) holds several images, one per `toolchains/<name>/` directory with a `Dockerfile`, each released as its own `<name>/vX.Y.Z` line:

```nohighlight
devctl gen circleci --repo-name agent-toolchains --language generic --flavour multi-image
devctl gen workflows --flavour multi-image --language generic --release-workflow auto-release
```

Every image is `giantswarm/<repo>/<name>` (`--image-name` sets the prefix), built with `toolchains/` as its context, so the images share the build files kept there beside them (`toolchains/lib/`).

- `.circleci/config.yml` is a setup workflow that reads the image directories at pipeline runtime, so a pull request that adds an image builds it without a regeneration, and continues the pipeline with one copy of the per-image workflow of `.circleci/workflows.yml` for each image to build (`__IMAGE__` in it stands for the name). The optional `.circleci/custom.yml` is merged in last, as everywhere.
- A branch pushes an amd64 dev image of each image whose directory changed against `main`; a change under `toolchains/` outside every image directory builds every image. `main` itself builds nothing: its merges are released by their tags.
- A tag `<name>/vX.Y.Z` publishes that image multi-arch (`--image-platforms`, `linux/amd64,linux/arm64` by default) to gsoci.azurecr.io under its version and `latest`; `--force-public` for a private repository whose images are public, `--image-private-only` for gsociprivate alone. A tag that names no image fails the pipeline.
- The repository's own check of an image is the job `check-image` in `.circleci/custom.yml`, with a string parameter `image`, the image's name: when it is defined, each release runs it first and the push requires it. Without it a release publishes straight away.

```yaml
# .circleci/custom.yml
jobs:
  check-image:
    parameters:
      image:
        type: string
    machine:
      image: ubuntu-2404:current
    steps:
    - checkout
    - run: scripts/check-toolchains.sh << parameters.image >>
```

`.github/workflows/zz_generated.auto_release.yaml` tags, on every push to the release branch, each image whose directory, or a shared file under `toolchains/`, changed since its latest `<name>/vX.Y.Z` with its next version, from the pushed commit's subject (the squash subject): a breaking change (`type!:` or a `BREAKING CHANGE` footer) the major, a `feat` the minor, anything else the patch, `v0.1.0` for a new image. There are no release candidates: each tag is stable and gets its GitHub release. `.github/release-gate.custom.sh`, when present, runs with each tag about to be cut; a refusal skips that tag and fails the run. The last step makes sure CircleCI built every tag, triggering a lost one through `CIRCLECI_API_TOKEN`. The flavour takes `auto-release` alone, and `cliff.toml` is removed.

## Adding a generator option

A repository's `gen` options are its entry in `repositories/<team>.yaml` of giantswarm/github. The align-files action (`.github/action/index.js` there) turns them into `devctl gen` flags: `installUpdateChart` is `--install-update-chart`, `helmDocsRegen` `--helm-docs-regen`, `ci.releaseWorkflow` `--release-workflow`. An entry without `ci.releaseWorkflow` gets `auto-release` when `ci.generate` is true and `legacy` otherwise, and pre-commit is generated when `ci.generate` is true or `preCommit` is a list (`[]` for the base hooks alone): a repository on GitHub Actions alone that wants Auto Release sets both keys explicitly.

A new option therefore lands in three steps:

1. In devctl: the flag, the template and its golden test. Each generator under `pkg/gen/input/<name>/` compares its output with goldens under `testdata/`, rewritten by `go test ./pkg/gen/input/<name>/ -update`; the workflow tests swap the provenance header for a fixed string (`withFixedHeader`), so a template that is not committed yet renders the same. The provenance files (`*.template.sha`) are gitignored and written by `go generate ./...`, which `make test` runs first.
2. In giantswarm/github, once the release is out: the devctl pin at that release or later, and the option on the repository's entry. A merged change to an entry aligns that repository; `gh workflow run align-files.yaml --repo giantswarm/github -f repository=<name>` aligns it again, and the run's log names the pull request it opened from `teams-alignment-branch`.
3. In the repository: review and merge that pull request, then prove the generated workflow with a pull request that exercises it.
