# Understanding flavours

`devctl` understands different types of GitHub repositories, to make the right modifications for each type. App repos have different needs than Go libraries, for example.

A flavour describes **what kind of project the repository is**. It is passed with `--flavour` (`-f`) and decides which files the `gen` commands write:

```nohighlight
devctl gen workflows --flavour app,customer
devctl gen makefile --flavour cli --language go
```

Flavours are not mutually exclusive. Some repos must be configured with multiple flavours -- pass them comma-separated, or repeat the flag. For example, an operator may carry the flavours `app` and `k8sapi`. Note: this configuration is usually persisted in our [repository configuration](https://github.com/giantswarm/github/tree/main/repositories).

The flavour is one of **two independent axes**. The other one is the programming language (`--language`), described in [Flavours and languages](#flavours-and-languages) below. Roughly: the flavour decides the release and packaging plumbing, the language decides the build and test plumbing.

## Which flavour do I need?

| Flavour | Pick it when the repo... |
|---------|--------------------------|
| [`app`](#app) | contains a Helm chart that is published to an app catalog |
| [`cluster-app`](#cluster-app) | is a cluster app (`cluster-aws`, `cluster-azure`, ...) |
| [`cli`](#cli) | ships downloadable binaries on its GitHub Release |
| [`k8sapi`](#k8sapi) | defines a Kubernetes API (CRDs) |
| [`fleet`](#fleet) | holds GitOps definitions for management clusters |
| [`customer`](#customer) | tracks issues on a shared customer board |
| [`generic`](#generic) | is none of the above and just needs to be released |

`generic` is the neutral choice, not a fallback for "I am not sure": it adds nothing beyond the base files. If a more specific flavour applies, use it -- and add `generic` only when nothing else does.

## Flavour reference

### `app`

An app, by the definition of the Giant Swarm app platform. Each app repository contains at least one Helm chart.

| Command | Result |
|---------|--------|
| `gen makefile` | `Makefile.gen.app.mk` -- the `lint-chart`, `update-chart`, `update-deps` and `helm-docs` targets |
| `gen workflows` | `.github/workflows/zz_generated.check_values_schema.yaml` |
| `gen workflows --install-update-chart` | additionally `zz_generated.update_chart.yaml`, plus `zz_generated.sync_from_upstream.yaml` with `--upstream-sync-automation` and `zz_generated.dispatch_update_chart_events.yaml` with `--dispatch-update-chart-events-repo` |
| `gen circleci` | the chart pipeline: `build-chart` / `push-to-app-catalog` jobs and the ATS chart tests |

With `--language kyverno-policy` this flavour also generates `zz_generated.test-kyverno-policies-with-chainsaw.yaml`.

### `cluster-app`

A specific type of app repository which provides a values schema that aims to fulfill the requirements of the [RFC #55](https://github.com/giantswarm/rfc/pull/55).

| Command | Result |
|---------|--------|
| `gen makefile` | `Makefile.gen.cluster_app.mk` |
| `gen workflows` | `.github/workflows/zz_generated.documentation_validation.yaml`, `zz_generated.json_schema_validation.yaml`, `zz_generated.diff_helm_render_templates.yaml` |

`cluster-app` only adds the schema and documentation validation on top of a chart repo -- it does not generate the chart build and publish pipeline itself. Combine it with `app` (`-f app,cluster-app`) to get both.

### `cli`

A command line interface (CLI) component which is typically executed on-demand either by a user or within an automation system. CLIs are typically released with downloadable and executable binaries.

| Command | Result |
|---------|--------|
| `gen makefile` | `Makefile.gen.go.mk` gains the packaging targets (`package-darwin-amd64`, `package-linux-arm64`, ... writing to `./bin-dist`), plus `.github/zz_generated.windows-code-signing.sh` |
| `gen circleci` | the `go-build` job builds the full six-architecture matrix (linux, darwin, windows on amd64/arm64) and an `upload-release-assets` job attaches the binaries to the GitHub Release on tag builds |

**Requires `--language go`** -- `gen makefile` rejects any other combination, and the CircleCI binary jobs are only emitted for Go.

Two `gen circleci` flags apply to this flavour only: `--build-concurrency` and `--resource-class`. Lower the first and/or raise the second if the cold cross-compile gets OOM-killed.

### `k8sapi`

A repository that provides a Kubernetes API (usually one or several custom resource definitions).

| Command | Result |
|---------|--------|
| `gen makefile` | `Makefile.gen.k8sapi.mk` (controller-gen based deepcopy/CRD generation), `hack/boilerplate.go.txt` and `hack/.gitignore`; removes the obsolete `Makefile.custom.mk` and `hack/tools/bin/controller-gen` |

### `fleet`

A repository to be used with GitOps containing kubernetes clusters.

| Command | Result |
|---------|--------|
| `gen workflows` | `.github/workflows/zz_generated.cluster_app_values_validation_schema.yaml` -- validates the `management-clusters/**/cluster-app-manifests.yaml` committed in the repo against the corresponding cluster app schema |

### `customer`

A repository used to track mostly issues and provide project boards, shared with a customer.

| Command | Result |
|---------|--------|
| `gen workflows` | `.github/workflows/zz_generated.add_customer_board_automation.yaml` -- adds new issues to the general customer board |

This flavour is purely about issue tracking. It says nothing about how the repo is built or released, so it is almost always combined with another flavour.

### `generic`

A repository that does not fit any of the more specific flavours above.

No flavour-specific files. The repo still gets everything that is not flavour-gated: the base `Makefile`, the release workflows, and whatever the `--language` selects.

## Flavours and languages

`--language` (`-l`) takes exactly **one** value and selects the toolchain:

| Language | Result |
|----------|--------|
| `go` | `gen makefile`: `Makefile.gen.go.mk`. `gen circleci`: the `go-build` job. `gen workflows`: `zz_generated.fix_vulnerabilities.yaml`. `gen llm`: `zz_generated.go-llm-rules.mdc`. `gen precommit`: repo name auto-detected from `go.mod` |
| `node` | `gen circleci`: a `node-build` / `node-test` job on a `cimg/node` executor, tuned with `--package-manager` and the `--node-*` flags. `gen precommit`: a `ci:lint` pre-push hook |
| `kyverno-policy` | `gen makefile`: `Makefile.gen.chainsaw.mk` and the chainsaw test scaffolding. `gen workflows` (with flavour `app`): `zz_generated.test-kyverno-policies-with-chainsaw.yaml` |
| `python` | accepted as a valid value, but no generator currently emits Python-specific files |
| `generic` | no language-specific files |

The two axes combine freely, apart from one rule: **`cli` requires `go`**. Some common pairings:

| Repo | Flags |
|------|-------|
| Go operator with a chart | `-f app,k8sapi -l go` |
| Cluster app | `-f app,cluster-app -l generic` |
| Go CLI tool | `-f cli -l go` |
| Kyverno policy chart | `-f app -l kyverno-policy` |
| Management cluster fleet repo | `-f fleet -l generic` |

### Caveats

- Only `gen makefile` and `gen circleci` validate `--language` against the list above. `gen workflows`, `gen llm` and `gen precommit` accept any string, so a typo such as `-l golang` silently produces output with the language-specific parts missing.
- `gen makefile` requires both `--flavour` and `--language`; `gen workflows` requires `--flavour`.
- `gen llm` accepts `--flavour`, but no generated rule currently depends on it. Only `--language go` changes its output.
- `gen renovate --language` is **not** this list. It names an ecosystem for Renovate to watch (`go`, `docker`, ...).
- `gen precommit` has its own, unrelated `--flavors` flag (US spelling) with the values `bash`, `md` and `helmchart`. These select extra pre-commit checkers and have nothing to do with repository flavours.
