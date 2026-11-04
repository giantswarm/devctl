# Hand-written CircleCI configs on architect orb < 10

Snapshot: 2026-11-04. Refs https://github.com/giantswarm/devctl/issues/2812.
This is an inventory and follow-up queue; it changes no downstream config and runs no build.

## Scope and evidence

Read all 1,850 repositories visible to this run in the `giantswarm` organization, including forks and
archives, through paginated GitHub GraphQL reads. Of these, 705 were unarchived and 1,145 archived.
There were 906 default-branch `.circleci/config.yml` files: 152 had a sibling `.circleci/workflows.yml`
and were excluded as generated CI, following [devctl's CI model](release-wait.md#the-ci-model-and-the-artifact-names).
Of the remaining 754, 364 pinned `giantswarm/architect` below 10: **12 unarchived and 352 archived**.
Archived repositories are excluded from the bump queue. Another 267 pinned newer architect orbs, and
123 had no architect orb pin (an architect Docker executor image alone is not an orb).

The table below covers every unarchived match. Each config link contains the full inspected commit.
Read each candidate's workflow directory and release workflows at that commit, and all its open PRs
without search-index filtering. Reuse existing major bumps; older minor-bump PRs are not extra queue
entries. The two private repositories are listed with metadata only.

**RC promotion** means a configured workflow that puts the stable tag on an existing RC's commit.
None of these 12 has one: nine use legacy release-PR workflows and three have no release workflow.
The legacy wrappers call
[create-release.yaml](https://github.com/giantswarm/github-workflows/blob/3b61dc36bf2a126429b77597a2a4fef138054522/.github/workflows/create-release.yaml),
which tags the release branch's commit; it can publish an RC, but has no RC-promotion input. The two
older inline implementations have the same release-branch model. This does not rule out manual
tagging or external automation. Recheck before moving any repository to auto-release.

## Inventory

`Present` means every architect `go-build` invocation has the literal `architect` context;
`No job` means the config does not invoke that job. It says nothing about the caller's access to a
CircleCI context. `Legacy` and `None` describe the release workflows read, not a live release test.

| Repository / pinned config | Branch | Orb | RC promotion / release model | `go-build` context |
|---|---|---|---|---|
| [azure-ad-pod-identity-app](https://github.com/giantswarm/azure-ad-pod-identity-app/blob/bbd52995d9e642f976d92949a16fc3501c9d9ccf/.circleci/config.yml) | main | 6.14.1 | No / Legacy | No job |
| [azure-storageclasses](https://github.com/giantswarm/azure-storageclasses/blob/3a958c115cec4454e02a5e459dcbb50d6e78e847/.circleci/config.yml) | master | 6.14.0 | No / Legacy | No job |
| [cluster-api-gen](https://github.com/giantswarm/cluster-api-gen/blob/2e79e6474451a5787de4c5732b6905cbffc32469/.circleci/config.yml) | master | 4.1.0 | No / Legacy | No job |
| [employees](https://github.com/giantswarm/employees/blob/be4f2c34edd611b9a1e838f4ca609d9c72c4be02/.circleci/config.yml) (private) | master | 5.2.0 | No / None | No job |
| [helloworld](https://github.com/giantswarm/helloworld/blob/e4aea661663bcc761e8a3028c00bcca07705a03c/.circleci/config.yml) | main | 8.3.0 | No / Legacy | Present |
| [honeybadger-notepad](https://github.com/giantswarm/honeybadger-notepad/blob/10275dc8b0d4934e0eb6e37d9c47ef24d6131fe7/.circleci/config.yml) (private) | main | 9.5.5 | No / None | No job |
| [kubernetes-event-exporter](https://github.com/giantswarm/kubernetes-event-exporter/blob/9841b4f2afaaad326fb381799b14824d51f9523c/.circleci/config.yml) (fork) | main | 6.2.1 | No / Legacy | **Missing** on `go-build-kubernetes-event-exporter` |
| [luigi](https://github.com/giantswarm/luigi/blob/b07907ab01357f1b77b6d7ae8be4afce0abd2564/.circleci/config.yml) | main | 9.1.0 | No / Legacy | Present |
| [microendpoint](https://github.com/giantswarm/microendpoint/blob/1301794606e0af62e7674ba2852de68dc775c4c2/.circleci/config.yml) | main | 9.6.0 | No / Legacy | No job |
| [renovate-playground](https://github.com/giantswarm/renovate-playground/blob/a2a166ab5fdeeea413dd14cef1a63585b916f9c3/.circleci/config.yml) | main | 8.2.2 | No / None | Present |
| [to](https://github.com/giantswarm/to/blob/15a713df3b344c18ae3eba0eb0b0b146b03020a6/.circleci/config.yml) | main | 9.6.0 | No / Legacy | No job |
| [xfnlib](https://github.com/giantswarm/xfnlib/blob/6cc135e625629efdf928fcecc2bcc56633c86ef8/.circleci/config.yml) | main | 4.33.0 | No / Legacy | No job |

## One bump per repository

These are queued follow-ups, not completed migrations. Six reuse an open PR; six need a downstream
PR when taken from this queue. No new downstream issue or PR was opened by this inventory. For new
bumps, use the reviewed 10.x pin current when implementing (devctl main currently pins 10.16.7), and
review the orb's job parameters and contexts together. A pin-only diff is not evidence of compatibility.
Do not repair an already active PR as part of this inventory.

| Repository | Queued bump and accompanying work |
|---|---|
| azure-ad-pod-identity-app | Reuse https://github.com/giantswarm/azure-ad-pod-identity-app/pull/162; review both chart-publishing jobs' legacy parameters. |
| azure-storageclasses | New bump on `master`; review the chart job's legacy parameters and catalog targets. |
| cluster-api-gen | New bump on `master`; review the `go-test` context, which is currently omitted. |
| employees | New bump on `master`; review the `go-test` context, which is currently omitted. Validate config offline; its downstream workflow performs account operations and must not be triggered as an inventory check. |
| helloworld | Reuse https://github.com/giantswarm/helloworld/pull/173; review the singular `architecture` matrix, `multiarch`, and build-workspace dependencies against the new orb. |
| honeybadger-notepad | New bump on `main`; review the custom job's architect executor and image-publishing commands. Do not dispatch its manual publishing workflow to validate the inventory. |
| kubernetes-event-exporter | New bump **with `context: architect` added to `go-build-kubernetes-event-exporter` in the same change**. This is a GitHub fork with no root `FORK.md` at the inspected commit: establish its consumed-branch policy before editing downstream; do not assume an upstream branch is the Giant Swarm line. |
| luigi | Reuse https://github.com/giantswarm/luigi/pull/57; preserve the six-platform build and release-asset dependency. |
| microendpoint | Reuse https://github.com/giantswarm/microendpoint/pull/228; the team file marks it `deprecated`, so confirm its lifecycle before taking the bump. |
| renovate-playground | Reuse https://github.com/giantswarm/renovate-playground/pull/22; retain the build's architect context. |
| to | Reuse https://github.com/giantswarm/to/pull/14; review the `go-test` context, which is currently omitted. |
| xfnlib | New bump on `main`; review the `go-test` context, which is currently omitted. |

Existing PR heads at inspection (these were read, not changed or independently approved):

| PR | Full head |
|---|---|
| https://github.com/giantswarm/azure-ad-pod-identity-app/pull/162 | `fd4a819767f75bacbbac5fca6a9dbb5168f87752` |
| https://github.com/giantswarm/helloworld/pull/173 | `bda77394e2de9f01d4e39201f864d82c105246a9` |
| https://github.com/giantswarm/luigi/pull/57 | `edced51d4346318ec79ac07a24fdf0e9668e4c26` |
| https://github.com/giantswarm/microendpoint/pull/228 | `01a03ef26d023d87254f4582c68c0d0d6fb90f0a` |
| https://github.com/giantswarm/renovate-playground/pull/22 | `2105fd521618a5ca555800f8664b2c66dbf37e7f` |
| https://github.com/giantswarm/to/pull/14 | `f24be54c402b306428b9ed95aafa8f2db74acc72` |

## Fork lines checked

The [team-file snapshot](https://github.com/giantswarm/github/tree/8b032a0cb52d0ecdc361921dbe83f9120dd06f7e/repositories)
declares six repositories with the `fork` flavour. Read their consumed branches and root ledgers,
and the maintenance lines identified there. These add **no below-10 bump** to the queue:

| Repository / pinned ledger or config | Lines and orb pins |
|---|---|
| [substrate](https://github.com/giantswarm/substrate/blob/fa510bdf1ecc87768c06b429db056bea4e83e86a/FORK.md) | `giantswarm`: 10.10.0 |
| [substrate release-1.0](https://github.com/giantswarm/substrate/blob/d60beed9ec53f2af6ee0f6c8a5caabb14d28a2e5/.circleci/config.yml) | 10.6.0 |
| [substrate release-1.3](https://github.com/giantswarm/substrate/blob/40cb3786a78db479756ddb0fb94ff70cc8f2d55a/.circleci/config.yml) | 10.10.0 |
| [substrate release-1.9](https://github.com/giantswarm/substrate/blob/46311898ee228071adaab80209fd35270c9306a2/.circleci/config.yml) | 10.10.0 |
| [substrate release-1.16](https://github.com/giantswarm/substrate/blob/c7ba0699f112aff30a332d3c5bd9be207eca5553/.circleci/config.yml) | 10.10.0 |
| [kagent-upstream](https://github.com/giantswarm/kagent-upstream/blob/2725afd3a4506998ecd69be0717cc38882fa86ed/FORK.md) | `giantswarm`: 10.16.7 |
| [kagent-upstream release-1.0](https://github.com/giantswarm/kagent-upstream/blob/25f55ca1a778591e605f1195902d33953ea01551/.circleci/config.yml) | 10.6.0 |
| [kagent-upstream release-1.1](https://github.com/giantswarm/kagent-upstream/blob/b849d8f8e5315659708e72cd690cbf434a18bb90/.circleci/config.yml) | 10.10.0 |
| [kagent-upstream release-1.2](https://github.com/giantswarm/kagent-upstream/blob/7b14763e4ef8e190e6e75033911c1e715a88389c/.circleci/config.yml) | 10.10.0 |
| [agentgateway-upstream](https://github.com/giantswarm/agentgateway-upstream/blob/c38ac490fee2d8dc25fc08edb42fca7bb9d3d90f/FORK.md) | `giantswarm`: 10.10.0 |
| [go-selfupdate](https://github.com/giantswarm/go-selfupdate/blob/a78c15c12d8083934c04ab7334b0b8ee09f47e72/FORK.md) | `giantswarm`: 10.13.0 |
| [mcp-go](https://github.com/giantswarm/mcp-go/blob/f801b6cea1cf58130bd0b5cd5f35abdad3a2b247/FORK.md) | `giantswarm`: no CircleCI config, no publishing workflow |
| [py-kube-downscaler](https://github.com/giantswarm/py-kube-downscaler/blob/edd4ef77a5e01d91d4bfa2bb35974303be42cb54/.circleci/config.yml) | `giantswarm`: 10.10.0; no root `FORK.md` at this commit |

Also read substrate's existing `release-1.8` at
[`fe803596fb92eda989a702df265b01146e3193fa`](https://github.com/giantswarm/substrate/blob/fe803596fb92eda989a702df265b01146e3193fa/.circleci/config.yml):
10.10.0. It is not listed as an active maintenance line in the inspected ledger.
This inventory does not enumerate every upstream mirror, historical tag or non-fork backport branch.

## Why a pin bump needs review

The original failure is documented in https://github.com/giantswarm/app-catalog-cleanup-tool/pull/306:
`v0.4.0` and `v0.4.0-rc.2` named one commit, and orb **6.15.0** refused the multiple tags. Its
[6.15.0 tag preparation](https://github.com/giantswarm/architect-orb/blob/bbf1003c357761b65b95501de89da3f7e5b2e8e1/src/commands/image-prepare-tag.yaml)
calls `architect project version`, whereas
[10.0.0](https://github.com/giantswarm/architect-orb/blob/3b67b0ef8168b2837ac10f91a827ba3ddbc795f8/src/commands/image-prepare-tag.yaml)
uses `gitsemver get`. The repository was fixed by commit
[`4829955b3998cb9ee94e66f1624940271f17196e`](https://github.com/giantswarm/app-catalog-cleanup-tool/commit/4829955b3998cb9ee94e66f1624940271f17196e)
and is excluded from the old-orb queue. This is downstream incident evidence, not a claim that the
inventory issue was already fixed on devctl main.

That fix also restricted image publishing to release tags: the 10.x job failed on branches when its
registry configuration allowed no development-image push. Preserve each repository's intended branch
publishing behavior when reviewing a bump. Include the missing build context identified above, review
removed job parameters, and require the downstream PR's CI. If a repository adopts RC promotion,
subsequent authorized release proof must check both the RC image and the stable image from the same
commit; this inventory performed no release, registry push, workflow dispatch or live account operation.

## Repeating the inventory

Use a read-only organization query with `repositories(first:25, after:$endCursor)` and
`pageInfo { hasNextPage endCursor }`, continuing until `hasNextPage` is false. For every repository read
`nameWithOwner`, `isArchived`, `isFork`, `defaultBranchRef { name target { oid } }` and the Blob at
`HEAD:.circleci/config.yml`; check for `HEAD:.circleci/workflows.yml` to distinguish generated CI.
Select actual orb declarations with numeric major < 10, not references to architect container images.
Read the selected repositories' workflow files at their captured full commits, follow any reusable
release workflow, and list every open PR with full head and base before creating a bump. Read the
team-file fork entries and each consumed line's `FORK.md`, then inspect its maintenance branches.
Report inaccessible reads and unknown promotion models explicitly rather than treating them as absent.

Validation of this snapshot checked pagination completion and repository uniqueness, all 12 pinned
configs and release models, the one missing `go-build` context, six existing major-bump PRs, and the
fork lines above. These are source checks; no downstream compatibility, CircleCI execution or live
promotion was tested. Keep the issue open until the queue is taken and the needed downstream proof exists.
