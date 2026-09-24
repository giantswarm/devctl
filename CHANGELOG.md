# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `gen circleci`: `build-chart` also exempts the images the repository's `.circleci/custom.yml` pushes with
  `architect/push-to-registries` jobs, derived from the checkout like the Dockerfile probe. A chart that deploys such an
  image at the stamped `appVersion` (a backend beside the app) failed `build-chart` on app-build-suite 2.5.0: the chart
  is packaged before that image is pushed, and the generated `ABS_HELM_IMAGE_REFERENCE_VALIDATOR_OWN_IMAGE` overrode
  any `.abs/main.yaml` entry. With more than one own image the variable carries a list (`"[<generated>, <custom>...]"`);
  a repository with one own image renders as before.
- `repo reconcile`: the run of the change that adds an entry builds the repository's first release. A repository
  created pull-request-last (`devctl repo create`, the repository manager) has its scaffold tagged `v0.1.0` before the
  entry merges, and the run of the merge is the first to follow the project on CircleCI, which therefore never saw
  the tag: since v8.71.0 that release was a `missed-tag-build` finding, and every created repository stayed without a
  built first release until a person triggered its pipeline. The release step now triggers the pipeline of a tag
  that names the scaffold commit (`feat: initial scaffold of <name> from `) in a run whose change added the entry,
  once: the next run finds the pipeline. Every other missed tag build, an adopted repository's included, stays a
  finding and is never rebuilt. `circleciclient.TriggerTagPipeline` is back for it.
- `gen circleci`: the generated pipelines pin architect orb 10.11.0, whose chart jobs run app-build-suite 2.5.0. In a
  repository that builds an image and stamps the chart's `appVersion`, `build-chart` no longer switches the image
  reference check off: its `pre-steps` step exports `ABS_HELM_IMAGE_REFERENCE_VALIDATOR_OWN_IMAGE` with the
  repository's own image (`gsoci.azurecr.io/giantswarm/<repo>`, or `gsoci.azurecr.io/<imageName>`). app-build-suite
  skips that image at the stamped version, which `build-chart` packages before the image is pushed, and resolves
  every other `gsoci.azurecr.io` reference. A missing third-party mirror tag goes red on the pull request again
  instead of at `push-chart-release`
  ([app-build-suite#624](https://github.com/giantswarm/app-build-suite/issues/624)). A chart that keeps the
  `appVersion` Chart.yaml declares is checked in full in `build-chart`; `push-chart` and `push-chart-release` check
  every reference as before.
- `gen circleci`: the generated pipelines pin architect orb 10.10.0, whose `architect` executor bundles gitsemver 3
  like its `app-build-suite` executor. From 10.8.0 to 10.9.0 a branch pipeline tagged its image with gitsemver 2's
  `X.Y.Z-dev.<branch>.<date>.<time>.h<sha>` and stamped its chart with gitsemver 3's `X.Y.Z-r<branch-hash>t<timestamp>h<sha>`,
  so the branch chart named an image that was never pushed and `execute-chart-tests` hung in `helm --wait`
  ([architect-orb#956](https://github.com/giantswarm/architect-orb/issues/956)). Dev image tags now follow the
  gitsemver 3 schema: a Flux filter that matches `-dev.<branch>.` stops matching new builds and selects a branch
  with `^.*-r<branch-hash>t[0-9]{14}h[0-9a-f]{7}$` (`gitsemver branch-hash <branch>`).
- `gen circleci`: the generated pipelines pin architect orb 10.9.0. Its `go-build` takes the compile parallelism from
  the executor's cgroup CPU quota instead of the host's `nproc`, which since 10.6.1 ran `-p 36` on a 2-vCPU `medium`
  executor and killed heavy builds in `Build binaries`
  ([architect-orb#952](https://github.com/giantswarm/architect-orb/issues/952), 10.8.1). On tag builds `go-test`
  stamps `pkg/project.version` with the tag, and the files the build writes no longer mark the build info `+dirty`
  (10.9.0). Its chart jobs run app-build-suite 2.4.1, which fails a chart whose rendered `gsoci.azurecr.io` image
  references the registry does not carry (10.8.0).
  `build-chart` packages the chart before the pipeline pushes its own image, and a branch without `branchPublish`
  never pushes it. So in a repository that builds an image, `build-chart` sets
  `ABS_DISABLE_HELM_IMAGE_REFERENCE_VALIDATOR=true` in a `pre-steps` step, and the check runs in `push-chart` and
  `push-chart-release`: they require the image job and run app-build-suite again before they publish. A chart of
  images built elsewhere keeps the check in `build-chart`. A template repository's `build-chart` sets the variable
  on the job, because the chart it renders pulls fixture images no registry carries. No job, parameter or required
  check changes name.
- `deploy`, `pr approve-align`, `pr approve-merge-renovate` and `release create` act with the `giantswarm-devctl`
  App login by default, through `authstore.ResolveGitHub`: no personal token is needed after `devctl auth login
  --github-only`. A token in `DEVCTL_GITHUB_TOKEN`, `GITHUB_TOKEN` or `OPSCTL_GITHUB_TOKEN` overrides the login and
  prints the resolver's warning once; with neither they exit 8 naming `devctl auth login --github-only` instead of
  failing with an error about `GITHUB_TOKEN`. `release create` resolves one token and builds every GitHub client of
  the release from it. A 404 from GitHub in `deploy` or `release create` under the App login names its likely cause:
  the App reaches the giantswarm organization and public repositories only, a token in the environment reaches the
  rest (`authstore.GitHubNotFoundHint`, `githubclient.ExplainNotFound`). `deploy` passes the token to git as basic
  auth instead of in the remote URL, so a git error quoting the URL no longer prints it
  ([#2380](https://github.com/giantswarm/devctl/issues/2380)).

### Added

- `reposetup.(*Schema).FieldValues(path)` returns the values the repositories schema allows for a declaration field
  (`componentType`, `gen.flavours`, `gen.language`, `visibility`, `lifecycle`), in the schema's order, read from the
  document the `Schema` was compiled from with its `$ref`s followed, so the embedded schema and one fetched from
  giantswarm/github each answer from themselves. An unknown path or a field without an enum is an error.
  `EmbeddedFieldValues` is built on it, and a test pins the schema's `gen.language` to `gen.AllLanguages()`.
- `gen.ci.templateContent: true` in a team-file entry says the `.circleci/config.yml` a template repository carries is
  content for the repositories created from it, not its own pipeline. The reconciler's circleci and release steps skip
  such a repository (`no CircleCI pipeline: .circleci/config.yml is template content`): no follow, no deploy key, no
  tag build to verify, and the branch is not read for it. It sits beside `generate: false`; `generate: true` beside it
  is refused by the validator, and the schema carries the field. Before, a template repository whose configuration carries placeholders (mcp-template's
  `{MCP-NAME}`) was treated like any repository with a configuration on its default branch, and aligning it would have
  made CircleCI follow it and run the placeholder pipeline on every push.
- `authstore.ResolveGitHub(ctx, envVars...)`, the one resolver of the GitHub token a command for people acts
  with: a token in `DEVCTL_GITHUB_TOKEN`, `GITHUB_TOKEN` or `OPSCTL_GITHUB_TOKEN` (or the one variable a
  `--github-token-envvar` names) is an explicit override that carries one warning naming the variable and
  `devctl auth login --github-only`; without one, the `giantswarm-devctl` App login from the keychain, refreshed when
  expired; neither is `ErrAuthRequired`, exit 8. With `CI` set the keychain is never read and nothing warns. It never
  falls back from one source to another and never asks `gh auth token`. The token names its `Source` (`keychain` or
  `$NAME`). `devctl auth status` warns when such a variable overrides the App login, its exit code unchanged, and
  `docs/auth.md` describes the GitHub token of every command
  ([#2379](https://github.com/giantswarm/devctl/issues/2379)).
- `repo reconcile`'s circleci step verifies the webhook CircleCI installs on the follow: after `followed, setup
  workflows on, checkout key present` it reads the repository's webhooks and requires one active hook for
  `https://circleci.com/hooks/github` with the `push` event, `webhook present` in the summary. A followed project
  without it is the finding `circleci-webhook-missing` (not advisory: no push and no tag reaches CircleCI, so no branch
  builds and the first release tag goes unbuilt), its fix naming what installs the hook -- a follow by a GitHub admin of
  the repository whose CircleCI grant carries the hook scope, `POST /api/v1.1/project/github/{owner}/{repo}/follow`
  or Project Settings; devctl cannot create it, CircleCI signs it with its own secret. Webhooks the identity cannot
  read (a read identity without the `repository_hooks` permission) are `unchecked`, never guessed. Before, the
  reconciler's follow as architectbot under its temporary admin grant left a project followed with a deploy key and
  setup workflows but no hook, every branch build silently absent and the step reading `ok`. The inventory record's
  `circleci` facts carry `webhook` for the manager to fill from the step, and `devctl repo status` prints
  `webhook present` / `webhook missing` in the `circleci` line when it is set
  ([#2332](https://github.com/giantswarm/devctl/issues/2332)).
- The `plans` flavour: a team plans repository (versioned PRDs, their companion websites and the
  plan-workflow agent skills -- cabbage-plans, bumblebee-plans, atlas-plans and the like). An add-on
  flavour declared beside `generic` (`flavours: [generic, plans]`) with `language: generic` and
  `gen.ci.generate: false`; the generators produce nothing extra for it, and `DeriveTemplate` derives
  it the new template `giantswarm/template-plans` (its `{APP-NAME}` and `{TEAM-NAME}` placeholders
  replaced the same way `giantswarm/template-app`'s are), refusing any other language the same way
  language `node` is refused until its template ships. The embedded schema copy
  (`pkg/reposetup/schema/repositories.schema.json`) carries the flavour; the live schema in
  giantswarm/github and the Backstage "Plans" scaffolder preset land in companion pull requests.
- `devctl gen circleci --chart-release-gate-job <job>` (`gen.ci.chartReleaseGateJob` in the giantswarm/github team
  file): the generated `push-chart-release` job requires the named repo-owned `custom.yml` job, the chart counterpart of
  `--image-pre-build-job`, for a check that has to refuse a release before its chart is pushed -- a meta chart whose
  component floor resolves to no published chart (giantswarm/agent-platform#624). The branch dev push is not gated; a
  repository without a chart release push refuses the flag.
- `circleciclient.Config.Anonymous`: a client without a token, for a reader that holds none -- it reads what CircleCI
  answers without one, the pipelines, workflows and jobs of a public project (a private one is 404), and sends no
  `Circle-Token` header; `New` still refuses an empty token without the flag, and a token with it, so a reader meant
  to hold a token never reads anonymously by accident. `circleciclient.WorkflowURL` names a workflow's page beside
  `PipelineURL` ([#2352](https://github.com/giantswarm/devctl/issues/2352)).
- The team-file entry declares the repository's own rulesets, the ones a team keeps beside the engine's: `rulesets`
  names them, and `repo reconcile`'s protection step leaves a ruleset named there alone and silent. A ruleset the
  repository carries and the entry does not name is the advisory `foreign-ruleset` as before, its fix naming the
  field; a name the repository carries no ruleset for is the new advisory `declared-ruleset-missing` -- a ruleset
  deleted on GitHub, or a name that never matched one -- so a stale declaration does not stand unread. Both leave
  `converged` true, and the engine still writes `devctl: default branch` and no other ruleset: creating, changing and
  deleting a declared ruleset stays with the team. Before, the fix text asked for a declaration the entry could not
  carry, so a repository that keeps a ruleset knowingly -- the fork lines' `protect-giantswarm`, `protect-main`,
  `protect-mirror-main` and `protect-consumed-branch`, which guard the upstream mirror and the consumed branch --
  carried the finding on every run for ever ([#2344](https://github.com/giantswarm/devctl/issues/2344)). The embedded
  schema copy (`pkg/reposetup/schema/repositories.schema.json`) carries the field; the live schema in
  giantswarm/github accepts it once its own change merges.

### Changed

- The version check that precedes every command, `version check`, `version update` and `repo validate` read GitHub
  with the `giantswarm-devctl` App login when there is no token in the environment, through
  `authstore.ResolveGitHub`: a token in `DEVCTL_GITHUB_TOKEN`, `GITHUB_TOKEN` or `OPSCTL_GITHUB_TOKEN` (`repo validate
  --github-token-envvar` names one variable instead; its default is now those three) overrides it with the resolver's
  warning, printed once per invocation. The token stays optional: with neither the reads stay anonymous (`repo
  validate`: the embedded schema, names unchecked), and with `CI` set the keychain is never read. The version check
  resolves the token only when it asks GitHub, never while its one-hour cache is fresh. `pkg/updater` reads releases
  through its own GitHub source, so the selfupdate library no longer picks up `GITHUB_TOKEN` by itself, and follows
  `DEVCTL_GITHUB_API_URL` ([#2381](https://github.com/giantswarm/devctl/issues/2381)).
- A command for people that fails with an error carrying its exit code (`agentcli.ExitCoder`) exits with that
  code: `ErrAuthRequired` is exit 8 for every command, its one sentence on stderr, so a `repo` command without a
  muster login exits 8 as `docs/auth.md` says. Before, every such error was exit 2
  ([#2379](https://github.com/giantswarm/devctl/issues/2379)).
- A command called the wrong way says what is wrong and how to call it, without a stack trace: a missing
  argument is named from the usage line (`Missing [OWNER/]REPOSITORY`), an extra one is `Unexpected argument "b"`,
  an unknown command or flag, or a flag the command's validation refuses, is followed by the command's usage line
  and `Run 'devctl repo status --help' for more information.` An unknown subcommand of a group (`devctl repo
  statsu`) is an error with the subcommands it resembles (`did you mean "status"?`) and exit 2, where it printed
  the group's help and exited 0. Every command declares the arguments it takes, so a stray argument to a command
  that takes none (`devctl gen circleci x`) is refused instead of ignored. The stack trace of an error is printed
  only at `--log-level debug`. The notice that a newer devctl is released is one line naming the version, the
  update command and `DEVCTL_UNSAFE_FORCE_VERSION`.
- `devctl pr merge` waits for the release its merge triggers: after the merge it runs the wait of `release wait --pr`
  on the merge commit it produced, so one blocking call returns when CI was green, the pull request is merged and the
  release is pullable (its images and charts resolve to a digest, its tag pipeline is green). The document gains
  `release` (the release wait's `verdict` and `reason` with its result: tag, sha, models, artifacts with digests,
  pipeline); `--release-timeout` bounds that wait (30 minutes) apart from `--timeout`; `--no-release-wait` ends the
  command at the merge. A merge that no release follows is exit 0: a repository that does not tag merge commits, or one
  whose auto-release run finished without a tag. After a merge, exit 6 (`release_failed`) says the release failed and
  exit 9 (`release_unconfirmed`) that it was not confirmed in time or could not be judged; both mean merged, never
  merge again. The agent no longer needs a second `devctl release wait --pr` call after its merge.
- `devctl release wait --pr` reads the merge commit's auto-release run while no tag is on the merge commit: a run that
  finished without a tag ends the wait at once as exit 3 with the new verdict `no_release` instead of a timeout, a run
  that failed before it tagged is exit 1, a run cancelled by a newer push is exit 3 naming the superseding push. A
  legacy repository's `--pr`, and one without any release workflow or declared release model, is `no_release` too.
- `release wait` declares a CircleCI release `available` only when every artifact resolves **and** the tag
  pipeline is green: every workflow (newest run per name) finished, none failed. The artifacts the team-file entry or
  the push jobs name are not the whole release: a repository's own tag jobs in `.circleci/custom.yml` push and sign
  more (vm-manager's guest image, muster's CRD chart, backstage's control-plane catalog entry), and `release wait
  giantswarm/vm-manager --pr 78` answered `available` for v0.22.3 while its `guest-image` job was still running.
  Artifacts that resolve under a running pipeline now keep the wait polling (`every artifact is available; pipeline
  N unfinished: …` on `--progress`); a tag job that fails after them is exit 1 with the job in `failedJobs`, a
  pipeline that does not finish in time exit 2 saying the artifacts are there. The Aliyun mirror is part of the
  wait; it typically ends within a minute of the chart push. An exit 0 document never lists an unfinished workflow
  ([#2364](https://github.com/giantswarm/devctl/issues/2364)).
- `gen circleci`: the generated pipelines pin architect orb 10.6.3, whose `image-prepare-tag` fails a branch pipeline at a
  tagged commit instead of resolving the release version and publishing it again
  ([architect-orb#942](https://github.com/giantswarm/architect-orb/issues/942)).
- `devctl repo reconcile` writes the repository admins (GitHub's repository role Admin) as a third bypass actor of the
  ruleset `devctl: default branch`, in `pull_request` mode beside the devctl App and the owning team: `devctl pr merge`
  run by an admin of the repository merges their own green pull request through the ruleset in every aligned
  repository, as classic protection without `enforce_admins` let them, every bypass in the audit log. A ruleset written
  before gains the actor on the next run; `agentMerge: false` still leaves the list empty. The decline's reason, the
  help and the docs name the role.

### Fixed

- `repo reconcile`: the `lifecycle` step leaves CircleCI before it archives or deletes the repository on GitHub, and
  unfollows and stops the project under the same admin grant as the `circleci` step ("grant architectbot admin for
  leaving CircleCI, revoked after it"). CircleCI takes "stop building" only from a GitHub admin of the repository,
  and an archived repository's collaborators are read-only. Before, the step archived first and stopped the project
  without the grant, so a push-only identity got `403 Permission denied` and the archive stayed half done
  ([#2409](https://github.com/giantswarm/devctl/issues/2409)). A repository that is already archived on GitHub and
  still followed on CircleCI is unarchived for the grant and archived again after it, also when CircleCI refuses.
- `gen circleci`: a `--chart-name` that differs from the repository name beyond an `-app` suffix also sets
  `explicit_allow_chart_name_mismatch: true` on every chart job, so the orb's chart name check no longer fails it.
- `repo reconcile`: the `circleci` step's admin grant for the CircleCI token's GitHub user covers every CircleCI write
  the step makes, not the follow alone. CircleCI takes the follow, the setup-workflows setting and a deploy key only
  from a GitHub admin of the repository. The step grants admin once, before its first write, when the user is not an
  admin already ("grant architectbot admin for the CircleCI set-up, revoked after it"), and revokes the grant when the
  step ends, also after a failed write. Before, the grant was revoked right after the follow, and a followed project
  missing its setup workflows or its deploy key was repaired without it.
- `version update` replaces the running devctl, symbolic links resolved, with a single rename
  (`selfupdatecosign.Install`). go-selfupdate's swap renamed the binary to `.devctl.old` before it moved
  `.devctl.new` in, so a `devctl` started in between found no binary, and two updates at once shared those names:
  three updates of one binary lost it in most runs. Now a `devctl` started meanwhile runs the old binary or the new
  one, any number of updates can run at once, no other file is touched, and the binary keeps its mode.
- `gen precommit` skips protobuf-generated code in every hook: a top-level `exclude` matches `*_pb.*`, `*_pb2*.py`
  and `*.pb.go` with its variants. `end-of-file-fixer` rewrote the buf-generated `*_pb.ts` files of a repository on
  every run, their next generation undid it, and the generated `pre-commit` check stayed red, so align-files could
  not land there. Hand-written files next to the output, a `gen/` directory's `README.md` and `buf.gen.yaml`
  included, are still checked, and so is `zz_generated.app-platform.values.yaml`, which triggers the helm-schema hook
  ([#2392](https://github.com/giantswarm/devctl/issues/2392)).
- `gen renovate` reads the repository of the `github>giantswarm/<name>:renovate-custom.json5` extends entry from
  the git origin remote when `--repo-name` is not given (`git remote get-url origin`, the `giantswarm/<name>` path
  of an https or ssh URL, `.git` stripped), no longer from the working directory's name. A worktree or a clone in
  a directory not named after its repository (`valkey-app-79`) wrote `github>giantswarm/valkey-app-79:…`, a preset
  Renovate cannot resolve, and the repository's Renovate stopped on a config-validation error. Without an origin
  remote, or with one outside the giantswarm organization, the command fails and asks for `--repo-name`; it never
  falls back to the directory name. The name is read only when `renovate-custom.json5` exists, the one case the
  generated config names the repository. `gen workflows` reads cliff.toml's `[remote.github].repo` with the same
  parser, so an origin that names no `<owner>/<name>` (a local path) renders `repo = ""` like a missing one
  ([#2389](https://github.com/giantswarm/devctl/pull/2389)).
- `devctl release wait` (and the release wait of `devctl pr merge`) probes a chart under the `name` its
  `helm/<dir>/Chart.yaml` declares at the tag, where `<dir>` is `gen.ci.chartName` or the repository (generated CI)
  or the push job's `chart` parameter (hand-written CI); `--catalog` looks the same name up in the index. Both only
  choose the directory the architect orb packages, and `helm push` names the OCI repository after the packaged chart.
  Before, a repository renamed after its chart was created (`helm/<old-name>`, `name: <repo>`) was probed under the
  directory name and never confirmed: exit 2, exit 9 after a merge. A `Chart.yaml` that is missing, does not parse or
  declares no name is exit 7 naming the file ([#2388](https://github.com/giantswarm/devctl/issues/2388)).
- `devctl pr wait`, `devctl pr merge` and `devctl release wait` wait for a spent rate limit instead of ending on it
  with exit 7: a GitHub or CircleCI read refused with `403` or `429` and `X-RateLimit-Remaining: 0` is sent again a
  second after `X-RateLimit-Reset`, one with `Retry-After` (a secondary limit, CircleCI's `429`) that much later,
  each a warning naming the limit and the time it resets. A reset after the wait's deadline ends the wait at once
  with exit 2 (9 after a merge), the reason naming the reset and the deadline. go-github's own bookkeeping no longer
  refuses the reads after an answer that spent the budget ("not making remote request"). A `403` with budget left is
  an answer, as before ([#2373](https://github.com/giantswarm/devctl/issues/2373)).
- The embedded schema copy (`pkg/reposetup/schema/repositories.schema.json`) carries `gen.ci.chartReleaseGateJob`,
  the team-file key for `gen circleci --chart-release-gate-job`. Validation against the embedded copy, which
  giantswarm-repo-manager runs on every declared entry, refused an entry setting it as `not a field of the
  repositories schema` (`entry-refused`, no set-up checks), while giantswarm/github's schema carries the key and the
  reconciler aligns the repository with it. A test holds every `gen circleci` flag a team file sets to a `gen.ci` key
  of the embedded copy.
- A created repository's scaffold passes `gen.ci.chartReleaseGateJob` to `gen circleci` as
  `--chart-release-gate-job`, as align-files does, so its first CircleCI configuration gates the release chart push
  on the declared job. Before, the scaffold left the key out and the gate appeared only with the first align. A test
  holds every `gen.ci` key of the embedded schema to the flag the scaffold's `gen circleci` line passes.
- A newer devctl release no longer breaks the agent-facing commands' contract: `pr wait`, `pr merge`, `release
  wait`, `auth login` and `auth status` run the version check after their argument checks and report an outdated
  devctl in their document, exit 7 with the reason naming `devctl version update` and `DEVCTL_UNSAFE_FORCE_VERSION`.
  Before, the check that precedes every command printed its error for a person and devctl exited 2, the code of a
  `timeout`, with no document, from the moment a release was published until the binary was updated.
- The agent-facing commands (`pr wait`, `pr merge`, `release wait`, `auth login`, `auth status`) answer a flag that
  does not parse (`--timeout 30`, an unknown flag) and wrong arguments with their JSON document and exit 7 (`usage`),
  the flag error naming `--help`. Before, cobra printed the error and devctl exited 2, the code of a `timeout`, with
  no document; `release wait` did the same for a missing or extra argument, and `auth login` and `auth status`
  for any argument.
- `devctl repo status` and `devctl repo get` read a record whose declaration the schema refuses: the manager keeps the
  declaration's `problems` as `field: message` strings (its inventory record, the Dev Portal's `InventoryRecord`),
  where devctl expected `{field, message}` objects and failed every such record with `cannot unmarshal string into Go
  struct field Declaration.declaration.problems`. A write's dry run keeps its problems as objects, as the manager
  answers them.
- `devctl version update` and `devctl version check` ask GitHub for the latest release every time and refresh the
  version cache with the answer. Right after a release the one-hour cache still named the older version, so an explicit
  update answered "You are already using the latest version." until `--no-cache` or the hour passed
  ([#2370](https://github.com/giantswarm/devctl/issues/2370)). The check before every other command keeps using the
  cache within its hour; `--no-cache` now only leaves the cache as it is.
- `devctl pr wait`, `devctl pr merge` and `devctl release wait` retry a read that GitHub or CircleCI did not answer
  instead of ending on it: a reset connection, an EOF, a try over 60 s or a 5xx is sent again after 2 s, the pause
  doubling up to 60 s, for up to eight tries in a row (about three minutes), never past the wait's timeout, each
  retried failure a warning with its time. A read that fails all eight tries is exit 7, the reason naming the request,
  the count and the last failure (`Get ".../pulls/42": failed 8 times in a row: 500 Internal Server Error`). Before, a
  single `connection reset by peer` from CircleCI or a single 500 from GitHub on any poll ended a thirty-minute wait
  with exit 7 while the head was on its way to green, and `pr merge` refused a merge it could have made. Only GET and
  HEAD are retried; the merge, the branch update and the branch deletion are sent once. The e2e mocks answer
  `reset: true` with a TCP reset ([#2359](https://github.com/giantswarm/devctl/issues/2359),
  [#2361](https://github.com/giantswarm/devctl/issues/2361)).
- `devctl pr merge`'s refusal of another human's pull request (exit 5) names every author it accepts -- the caller,
  bots and GitHub Apps (the `Bot` user type or a `[bot]` login) and the automation accounts `architectbot` and
  `taylorbot` -- where it said "the caller's own pull requests and bots' only", which left a reader guessing whether
  a release pull request counted; an automation login whose account id is not the pinned one is named with both ids,
  so the login alone opening nothing is visible in the reason ([#2340](https://github.com/giantswarm/devctl/issues/2340)).
- A repository created with `devctl repo create` with the `app` flavour merges its first pull request on green CI. The
  scaffold carries the chart tests its generated pipeline's `execute-chart-tests` job runs, beside the generated
  `tests/ats/pyproject.toml`: `.ats/main.yaml`, which skips the functional scenario and the upgrade scenario (a new
  repository has no released chart to upgrade from), and `tests/ats/test_smoke.py`, one `smoke` test that the job's
  kind cluster is reachable. Before, app-test-suite picked the pytest executor from the generated dependency file and
  the job failed in the smoke scenario's pre-run -- "Pytest tests were requested, but no python source code file was
  found" -- on the first pull request of every new chart repository; with a test in place the upgrade scenario refused
  next, having no released chart to upgrade from. Both files are the repository's own: a template that carries a test
  or the configuration keeps it, and an align run never writes them
  ([#2354](https://github.com/giantswarm/devctl/issues/2354)).
- `devctl release wait` waits out a tag pipeline whose jobs CircleCI does not list yet. Right after the auto-release
  tags a merge, CircleCI knows the pipeline's setup workflow by id but answers 404 on `GET /workflow/{id}/job` for a
  short while; the wait read that as a tooling failure and ended with exit 7 (`reading the jobs of workflow setup:
  not found`), the bounded wait it was given unused, while the same call minutes later answered `available`. A 404 on
  the jobs of a workflow that has not finished is now the tag not built yet: the poll goes on within the timeout,
  the document's new `pipeline.unfinished` names the workflow (`setup (running, jobs not visible yet)`) beside every
  other workflow still running, and a timeout's reason names them too. The same 404 on a finished workflow stays
  exit 7 ([#2345](https://github.com/giantswarm/devctl/issues/2345)).
- `devctl release wait` judges a repository by the workflows at its tag when the team-file entry says otherwise:
  they made the tag. A repository whose generated pipeline and auto-release workflow merged while its entry still
  resolved `legacy` -- the declaration lands in giantswarm/github later, by a person -- was refused with exit 7 in both
  the `--pr` and the version form for the hours between the two merges, although the tag, the release and its green
  pipeline existed. The workflows now decide (the reverse, an entry switched before align-files rendered the
  workflow, the same way), and the document's `warnings` names the mismatch and the remedy for the lagging side:
  `declaration says legacy, repository runs auto-release: the team-file entry resolves gen.ci.releaseWorkflow to
  legacy while the workflows at <sha> are the auto-release ones (…); the repository's workflows decide, align the
  team-file entry …`. An entry without a `gen.ci` block names the artifacts of its generated pipeline through the
  generator's defaults, where it was refused for the missing block; a repository no team file declares whose tag
  carries the generated pipeline is exit 7 naming that, where it crashed. The CI model is still cross-checked: an
  entry with `gen.ci.generate` set has to match the files
  ([#2333](https://github.com/giantswarm/devctl/issues/2333)).
- `repo reconcile`'s release step counts only the newest run of every workflow of the tag's pipeline, as `release
  wait` does: a rerun from failed is a second workflow of the same name, and the run it replaced keeps its failed
  status for ever, so a tag revived by a rerun read `red-release` until the next tag. The finding's fix no longer
  calls the tag dead: it names the rerun from failed (the rerun checks out the same commit and runs the publish
  jobs) and `devctl release wait` to confirm, the next tag only when the cause is in the code
  ([#2352](https://github.com/giantswarm/devctl/issues/2352)).
- `devctl pr merge` merges the release pull requests Giant Swarm's automation opens. `taylorbot` and `architectbot`
  are plain GitHub user accounts -- no `[bot]` login, no `Bot` user type -- so the author check could not tell them
  from a teammate and refused every one with exit 5, "opened by taylorbot, not by you". taylorbot opens the
  `chore(release): vX.Y.Z` pull request of every repository on devctl-generated CI (the
  `zz_generated.create_release_pr.yaml` workflow), so the refusal blocked an agent from finishing a release in any
  repository. Both accounts are now matched on their numeric account id as well as their login, so the login alone
  opens nothing: an account that ever takes a released login is refused like any other person's. The rest of the check is unchanged -- a teammate's pull
  request is still exit 5 -- and the automation that already held a `[bot]` login (`renovate[bot]`,
  `giantswarm-align-files[bot]`, `giantswarm-marge[bot]`, `heraldbot[bot]`, `giantswarm-mctlbot[bot]`) merged before
  and merges now.

- `repo reconcile`'s protection step compares a ruleset's bypass list only when the identity can read it: GitHub
  returns `bypass_actors` to an identity with write access to the ruleset alone, so a read identity (giantswarm-repo-manager's
  inventory App behind `repo status`) got the ruleset without the field, the step compared an empty list against the App,
  the repository admins and the owning team and reported every aligned repository as `drift: bypass actors: …`,
  giantswarm/devctl included, whose ruleset carries exactly those. An absent list is now told from an empty one: the
  rules are compared, the bypass list is not, and the summary says `bypass actors not readable by this identity, not
  compared`; an identity that reads the list compares and writes it as before (#2346).

- `devctl repo reconcile`'s protection step passes over a repository ruleset whose enforcement is `disabled`: it
  enforces nothing, so it neither conflicts with `devctl: default branch` nor leaves a person anything to weigh, and
  the advisory `foreign-ruleset` no longer names it on every run. A ruleset on `evaluate` is reported like an active
  one, its rules being live in the audit log. Before, every ruleset the engine did not create was reported whatever
  it enforced -- a disabled Copilot review ruleset as loudly as an active branch protection
  ([#2344](https://github.com/giantswarm/devctl/issues/2344)).
- `devctl repo reconcile --mode check` without `--devctl-app-id`, and with it giantswarm-repo-manager's read-mode
  engine behind `repo status`, reads a repository on the ruleset `devctl: default branch` as converged. The protection
  step reads the repository's rulesets first and compares the ruleset's rules with the write path's comparison, the
  bypass list excepted: the App id names one of its actors, so without it the list is neither compared nor written and
  the summary says `bypass actors not compared`. A difference, or a classic protection still standing beside the
  ruleset, is the finding `ruleset-pending` for the run that has the id, the reconciler's; nothing is written without
  it. A repository without the ruleset keeps its classic protection as before, with the advisory `rulesets-not-enabled`
  worded for today's model: the bypass actors are the owning team and the repository admins for people and the devctl
  App for the reconciler, `--devctl-app-id` the write path's remedy. Before, a run without the id went to the classic
  protection without reading the rulesets, found none on an aligned repository and planned to write one: every aligned
  repository read "not converged", the advisory naming a switch made long since (#2341).
- A merge `devctl pr merge` has declined for the review rule says whom devctl acted as (the user token of `devctl auth
  login`; the App's bypass covers installation tokens devctl never holds), which rulesets of the base carry a pull
  request rule and their bypass actors, and the team whose file declares the entry: one of its members or a repository
  admin merges it, or a reviewer with write access approves it first. The help and `docs/pr-merge.md` said the App's
  bypass passed the review rule.
- `repo reconcile`'s release step verifies the latest release only when its tag is a `vX.Y.Z` tag (a pre-release
  suffix allowed): the shape auto-release cuts and the generated pipeline's `/^v.*/` filter builds. A latest release
  tagged otherwise -- per component, `base/v0.1.0` -- ends the step `skipped` naming the tag, with no CircleCI request and
  no finding, where it was reported as `missed-tag-build` the repository could never clear
  ([#2330](https://github.com/giantswarm/devctl/issues/2330)).

- `devctl repo reconcile` and `repo status` read `gen.ci.generate` as the entry declares it: an existing entry with
  `gen` but no `gen.ci` keeps the repository's own CircleCI configuration, as the schema says, and the circleci and
  release steps run only when `.circleci/config.yml` is on the default branch. The validator writes the creation
  default `gen.ci.generate: true` for an entry being added only (`repo create`, `repo validate --mode create`); in
  existing mode the entry is rendered as declared. Before, the default reached the engine for every existing entry:
  a repository released by GitHub Actions was planned a CircleCI follow with a deploy key, and its release reported
  as a missed tag build.

- `devctl repo reconcile`: the settings step keeps rebase merges on a fork line (flavour `fork`) and leaves its merge
  commits as the repository has them; the rest of the settings baseline applies as everywhere. A fork line's carried
  patches land by rebase merge, one upstream-ready commit each, and a re-pin merges upstream's history: with the
  squash-only baseline applied, GitHub refused the line's merges, so the fork lines could not opt in to alignment.

- The scaffold step's `abs-prerequisite` check reads the chart at `helm/<gen.ci.chartName>` when the entry sets the
  field, `helm/<repository>` otherwise: a repository whose chart is named otherwise (docs-proxy ships
  `helm/docs-proxy-app`, declared with `chartName: docs-proxy-app`) was reported without a chart on every check and
  could not converge. When the declared directory has no chart, the fix names the charts the repository has under
  `helm/` and the `gen.ci.chartName` remedy: a repository renamed on GitHub keeps its chart under the old name.

- `repo reconcile`: the catalog step no longer dispatches the apps-to-teams mapping for a chart whose reference is a
  template's placeholder (`{APP-NAME}`, `{MCP-NAME}`): the mapping's generator drops such a reference, so the dispatch
  changed nothing and the repository never converged. The scaffold step's chart check does not read the chart of a
  `componentType: template` entry, which lives under a placeholder directory and is built from a rendered copy by the
  template's own pipeline; it reported `abs-prerequisite: no chart at helm/<name>/Chart.yaml` before (#2326).
- The repo commands call giantswarm-repo-manager's tools through muster's `call_tool` meta-tool and unwrap its envelope,
  which is how muster exposes every server's tool to a session (the muster CLI and the platform's agents do the same):
  8.85.0 called the tools directly and muster answered `tool not found` for every one of them. The e2e muster mock now
  behaves like muster -- the meta-tools listed, server tools through `call_tool` in the envelope, direct calls refused --
  so the `repo-*` and `auth-login-muster` scenarios prove the real path. `auth login --muster-only` treats only
  `Server 'giantswarm-repo-manager' not found` as the manager's absence.
- `devctl auth login --muster-only` identifies devctl by its Client ID Metadata Document
  (`https://giantswarm.github.io/muster/devctl.json`, served next to the muster agent's) instead of registering a
  client at muster's `/oauth/register`, which gazelle's muster gates with a registration token: the sign-in was refused
  with `invalid_token` before any browser opened. A server that does not advertise
  `client_id_metadata_document_supported` is refused with the reason; the e2e muster mock accepts a metadata-document
  client id.

### Added

- `devctl repo list|get|refresh|sweep|info|watch|adopt|update|transfer|set-lifecycle|approve|align`: one thin
  subcommand per tool of giantswarm-repo-manager, called through muster as the person with the keychain's muster
  token (`devctl auth login --muster-only`). The manager's surface on the laptop is the page's and the agent's: the
  inventory listed, scoped and filtered; a record read or rebuilt; a new repository followed to readiness; an
  undeclared repository adopted; an entry edited field by field (`--set gen.ci.generate=false`) or replaced
  (`--entry-file`); a repository transferred, deprecated, archived or deleted (`--confirm`); a team-file pull request
  approved as a member; Align now in the mode the entry decides. Every write takes `--dry-run` and otherwise lands
  as a team-file pull request; `-o json` prints the manager's answer as it came. `pkg/reposetup/manager` carries the
  manager's answer types. e2e scenarios `repo-*` cover each verb against the mocked muster, with the arguments each
  call must carry pinned (`args` on a scripted tool answer) and text assertions (`stdoutContains`, `stderrContains`).

### Changed

- `devctl repo status` reads giantswarm-repo-manager's record only, with the keychain's muster token: the local
  fallback to the engine's checks and the flags `--muster-endpoint`, `--muster-token-envvar`, `--github-token-envvar`,
  `--circleci-token-envvar`, `--team` and `--owner` are gone (the local check is `devctl repo reconcile --dry-run`);
  without a muster token the command exits naming `devctl auth login --muster-only`. The text output gains the last
  reconciler run, the run awaited, a run that never reported and the inventory's own findings; `-o json` prints the
  record as the manager answered.

- `devctl auth login --muster-only`: the sign-in to muster for the `repo` commands, which call giantswarm-repo-manager
  through it. The authorization code flow with PKCE runs against muster's own authorization server, discovered from
  the endpoint (RFC 9728, RFC 8414); devctl registers itself there once per device, binds the token to the endpoint
  (RFC 8707) and refreshes it without a human; then it completes the sign-in to giantswarm-repo-manager (the GitHub
  App consent, once) and reports it under `giantswarmRepoManager`. The record is the third identity of the keychain,
  `muster`, with its endpoint; `auth status` reports it and does not require it. `--muster-endpoint` and
  `DEVCTL_MUSTER_URL` name the muster, gazelle's by default. `pkg/reposetup/manager` calls any tool of the manager
  through one MCP session and tells a refused bearer from an unreachable endpoint. The e2e harness gains a muster mock
  (its OAuth server and MCP endpoint) and `browser: true`, the harness playing the person at the browser; scenarios
  `auth-login-muster` and `auth-login-muster-no-manager`.

- e2e scenarios for `devctl pr merge`, the six paths the command encodes, each asserting the exit code and the JSON
  document against the mocked GitHub and CircleCI APIs: `own-green-merged` (the caller's own green pull request is
  squash-merged through the merge API and its branch deleted through the refs API), `other-human-refused` (exit 5
  before the wait, no team file read), `opt-out-refused` (the team-file entry says `agentMerge: false`: exit 5 naming
  the field), `behind-strict-base` (exit 3 without `--update-branch`) and `behind-update-branch` (the base merged into
  the head, the new head waited for and merged), `merge-queue` (enqueued through GraphQL, merged by the queue, the
  branch deleted), `red-not-merged` (exit 1, nothing merged) (#2278).

- e2e scenarios for `devctl pr wait`, the six field incidents the command encodes, each asserting the exit code
  and the JSON document against the mocked GitHub and CircleCI APIs: `stage-gap` (a CircleCI workflow behind
  `requires:` still running while the check list reads green ends green once it succeeds), `fork-awaiting-approval`
  (a fork's workflow run awaiting approval keeps the wait open; exit 2 names the run), `conflicting-pr` (exit 3
  before the first poll of the head), `retitled-stale-run` (a stale failed title check next to the passed rerun is
  green), `red-circleci-workflow` (the newest run of a workflow failed: exit 1 naming it), `pr-wait-timeout` (a check
  that stays queued: exit 2 naming it) (#2277).

- `devctl pr merge <owner/repo> <number> [--timeout 30m] [--rebase] [--update-branch] [--progress]`: the wait of
  `pr wait` (same verdicts, same codes), then a squash merge (`--rebase`: a rebase merge)
  through the merge API with the judged head as the expected head and the branch deleted through the refs
  API; a base with a merge queue is enqueued and waited for instead. Refused before any wait: a draft, closed
  or conflicting pull request and one behind a strict base (exit 3; `--update-branch` updates the branch and
  waits for the new head), a pull request another human opened (exit 5; bots, GitHub Apps and the caller are
  fine) and a repository whose team-file entry in giantswarm/github says `agentMerge: false` (exit 5, naming
  the field). No protection setting is read to be changed or written: the devctl GitHub App is a bypass actor
  of the rulesets. The document is `pr wait`'s plus `mergeCommitSha`, `method`, `branchDeleted` and
  `enqueued`. The engine is `pkg/prmerge` on `pkg/prwait`; `pkg/githubclient` gains the merge, update-branch,
  ref deletion, merge-queue rule and enqueue calls (#2278).

- `reconcile.Result.Requests` (`"requests": {"github": N, "circleci": M}` in the artifact, omitted when
  nothing was counted) and the count on the summary's header line: what the run cost in requests to each
  system, counted at the clients' transports through `reconcile.Counter`, an `http.RoundTripper` the CLI
  builds both clients over (`githubclient.Config.Transport`, `circleciclient.Config.Transport`) and hands
  to `reconcile.Runner.GitHubRequests` and `.CircleCIRequests`; the log names each step's cost (#2274).

- `devctl pr wait <owner/repo> <number> [--timeout 30m] [--progress]`: one bounded, blocking wait until the
  pull request's head is green as the merge box sees it, red, or in a state no CI can turn green, then one
  JSON document and an exit code (0 green, 1 red, 2 timeout naming what was unfinished, 3 draft/closed/
  conflicting/behind a strict base, 4 a required context never reported, 7 usage, 8 authentication). Green
  is the latest check run per name (a rerun replaces a stale failed run), every CircleCI workflow of the head
  revision read from CircleCI (a job behind `requires:` has posted nothing to GitHub between stages), no
  GitHub Actions run open or awaiting a fork's approval, and every required status context of the base's
  protection and rulesets reported. CircleCI is consulted only when the head carries `.circleci/config.yml`
  and the project exists; an upstream fork is judged from GitHub alone. Polls are conditional requests
  (ETags; `githubclient.NewConditional`) at an interval from the rate-limit headers, 15 s to 60 s. The engine
  is `pkg/prwait`, for `pr merge` to reuse (#2277).

- `devctl repo reconcile --devctl-app-id` and `reconcile.Runner.DevctlAppID`: the numeric id of the devctl
  GitHub App (the App's settings page; not the client id) and the switch to rulesets. With it the protection
  step writes the default branch's repository ruleset `devctl: default branch` -- active on `~DEFAULT_BRANCH`,
  so a rename or a fork line's declared branch needs no change; the baseline's review requirement; the required
  checks on the reported-only rule, strict off, a GitHub Actions gate pinned to the GitHub Actions App; no
  deletion, no force push; the App as bypass actor in `pull_request` mode unless the entry declares
  `agentMerge: false` (then none, so nothing merges past the required review) -- and classic protection gives
  way in the same run: its checks carried over, the ruleset written, the classic protection removed; the dry
  run plans both, a second run plans nothing. A ruleset the engine did not create is left alone and reported
  as the advisory finding `foreign-ruleset`. Without the id the step writes classic branch protection as
  before, converged as before, and reports the missing id as the advisory finding `rulesets-not-enabled`: the
  reconciler's wiring passes the id, so a devctl release alone moves no repository to rulesets.
  `reposetup.Fields` gains `AgentMerge` (#2288).

- `devctl release wait <owner/repo> (<vX.Y.Z|X.Y.Z> | --pr <n>) [--timeout 30m] [--catalog] [--progress]`:
  block until a tag's images and charts are pullable (`pkg/releasewait`). The release model comes from the
  team-file entry (`gen.ci.releaseWorkflow`, defaulting from `gen.ci.generate`) cross-checked against the
  workflow files at the tag, a disagreement being exit 7 rather than a guess; `--pr` resolves the tag on the
  merge commit of an auto-release repository and is exit 3 on a legacy one. The artifact names come from the
  sources that define them: for generated CI what the generator emits for the entry (`gen.ci.image.name`,
  `gen.ci.chartName`, the app flavour, a Dockerfile at the tag), for hand-written CI the push jobs of the tag
  pipeline matched with the architect push jobs of the tag's `.circleci` files; a Dockerfile without an image
  among them is exit 7, never the repository name. A repository without image and chart is waited for through
  its published release and the tag's workflows. Availability is a digest: the public registry is probed
  anonymously (a stale docker login cannot produce a false UNAUTHORIZED), the private registry with the docker
  keychain, and any answer other than a digest or `MANIFEST_UNKNOWN` ends the wait as exit 7. A failed or
  cancelled workflow of the tag pipeline (newest run per workflow name) is exit 1 with the failed jobs; without
  CircleCI the Actions runs of the tag decide. One JSON document at the end; `docs/release-wait.md` has the
  document and the exit codes. `pkg/githubclient` gains the tag, release and directory calls; `pkg/circleciclient` the
  newest run per workflow name and the pipeline of a tag (#2279).

- `devctl auth login` and `devctl auth status`: the identities the agent-facing commands act with. GitHub
  through the device flow of the devctl GitHub App (a user access token refreshed by the commands themselves
  for six months, no client secret in the binary), CircleCI through the OAuth 2.0 authorization code flow with
  PKCE after a one-time dynamic client registration per device (a 90-day API token, no refresh; a seven-day
  expiry warning in every command's `warnings`). Both tokens live in the OS keychain (`pkg/authstore`: Secret
  Service, Keychain, Credential Manager; `DEVCTL_KEYRING_FILE` selects a 0600 JSON file for tests) and are
  never printed. `authstore.RequireGitHub` and `authstore.RequireCircleCI` are the gate the other commands
  call first: the token, or `ErrAuthRequired` as exit 8 naming `devctl auth login`. `pkg/agentcli` is what the
  agent-facing commands share: the JSON envelope, the exit-code table, the `DEVCTL_TIME_SCALE` clock, the
  endpoint variables and the `--progress` writer (#2276).

- `e2e/`: the end-to-end harness of the agent commands, run by `make test`. `TestMain` builds devctl once and
  every `e2e/scenarios/<slug>/` runs the binary against in-process mocks of the GitHub REST API (rate-limit
  headers, ETags, 304 on a conditional request), the CircleCI API v2 and an OCI registry (one public, one
  private; `MANIFEST_UNKNOWN`, the stale-login 401), each answering from the scenario's per-route response
  sequences: the Nth request gets the Nth response, the last repeats. `expected.json` asserts the exit code and
  the JSON document with `"*"` wildcards; `DEVCTL_TIME_SCALE=0.001` keeps a thirty-minute wait under two seconds.
  Adding a scenario is a directory with `scenario.yaml` and `expected.json`; `e2e/README.md` documents the format,
  the environment seams and the slugs of the known incidents (#2280).

- `defaultBranch` and the flavour `fork` in the repositories schema devctl ships and in `reposetup.Fields`.
  `defaultBranch` (default `main`) is the repository's default branch: the settings step keeps the repository on it
  and renames a default branch that is not the declared one, the protection step protects it, so a repository on a
  branch of its own declares it and keeps it. `fork` is a fork line, a repository that carries an upstream release
  plus the carried patches on the branch it declares: the scaffold and codeowners steps are skipped on it
  (`skipped: flavour fork`), the generators (`devctl gen makefile|workflows|llm|circleci`, and the scaffold's
  command lines) produce nothing for it, every other step runs as declared. `repo status` names the declared
  branch and flavours next to the opt-in (#2271).

- `e2e/scenarios/auth-missing`, `auth-expired` and `auth-refreshed`: `auth status` against the keyring states the
  gate of the agent-facing commands distinguishes: no record (exit 8 naming `devctl auth login`), both tokens
  and the GitHub refresh token expired (exit 8, both reported expired), a GitHub token expired under a live
  refresh token and a valid CircleCI token (exit 0, the GitHub identity `refreshable`). Nothing is contacted and
  no token material is asserted (#2276).

- `e2e/scenarios/renamed-image`, `hand-written-ci`, `release-assets-only`, `failed-tag-pipeline`,
  `rerun-replaces-failed`, `stale-registry-login` and `release-wait-timeout`: `release wait` against the seven field
  incidents, each asserting the exit code and the JSON document. Generated CI whose entry renames the image
  (`gen.ci.image.name`) is probed under the override and never under the repository name; hand-written CI names its
  two images through the tag pipeline's push jobs and both are probed; a repository with neither image nor chart is
  available through its published release and green tag workflows (`kind: release-asset`); a failed workflow of the
  tag pipeline is exit 1 with `pipeline.failedJobs`; a rerun of a failed workflow of the same name reads as green;
  a stale docker login to the public registry never reaches the anonymous probe; artifacts that never appear while
  the pipeline runs are exit 2 at the deadline. The registry mock's `staleLogin` refuses the requests that carry
  credentials and serves anonymous reads, the answer a public registry gives (#2279).

- `requiredChecks` in the repositories schema devctl ships and in `reposetup.Fields`: status-check contexts an entry
  requires on its default branch whatever reported, merged with the baseline's reported-only rule by the protection
  step and never removed as ghosts. For a repository's own GitHub Actions gate that runs on every pull request, such
  as a team-file validation job, which the rule could not require before it had reported (#2273).

- `lifecycle: deleted`, the fourth lifecycle of the repositories schema devctl ships, and the engine's handling of it:
  the lifecycle step unfollows the repository's CircleCI project and stops it building, then deletes the repository on
  GitHub -- code, issues, pull requests, releases and packages with it (an organization owner can restore it on GitHub
  for 90 days) -- and the entry stays in the team file as the record of the deletion. Every other step is skipped on
  the declaration (`lifecycle: deleted`); a declared deletion whose repository is gone is converged with nothing to
  report (`deleted, as declared`). The `repository-missing` fix names `lifecycle: deleted` as the record of a
  repository deleted by hand, in place of removing the entry (#2262).

- The repositories schema devctl ships (`pkg/reposetup/schema/repositories.schema.json`, the copy of
  giantswarm/github's `.github/repositories.schema.json`) knows `align`, the repository's opt-in to alignment: with
  `align: true` in its entry the reconciler changes the repository to its declared set-up on every trigger, without it
  every run is a check that changes nothing. `reposetup.Fields.Align` carries the field; `repo status` says whether
  the repository is opted in when the engine reads the entry (#2259).

### Changed

- `repo reconcile`: the protection step's ruleset names the repository's owning team as a second bypass actor
  beside the devctl App, both in `pull_request` mode -- the organization's team of the team file's slug, its id
  read once per run. GitHub evaluates a request made with the App's user access token as the person, not as the
  App, so the App's bypass alone let nobody merge through the API without a second review: a team member's own
  green pull request now merges through their token, direct pushes stay forbidden, every bypass is audited.
  `agentMerge: false` keeps the list empty as before; a repository already on the ruleset gains the team actor
  and nothing else; a secret team, which GitHub refuses as bypass actor, is reported as the finding
  `team-bypass-refused` naming the team and its privacy, and the ruleset is written with the App alone (#2315).

- `repo reconcile`, `repo setup`, `repo checks`: a check of a converged repository costs at most twenty GitHub
  requests (giantswarm/backstage 18, giantswarm/klaus 19 measured; the devctl App id's ruleset reads add two).
  The reported checks are the statuses and check runs of the newest merged pull request's head, from one page
  of the recently merged pull requests (three requests; the branch's tags and commits are no longer read: a
  check that reports only on a push to the default branch or on a tag never gates a pull request, and on an
  auto-released repository every commit is a tag); without a merged pull request nothing has reported and
  nothing is required or removed. The scaffold step reads the root listing alone (the head commit only when
  `repo create` reports it); the renovate step looks the installation up only for a repository without a trace
  of a run, and reads no trace on a repository younger than a day (#2274).
- `repo reconcile`, `repo setup`, `repo checks`: the discovery of the reported checks reads one page of the
  newest hundred tags (`per_page=100`) instead of walking the whole list ten per page, and the reconciler
  reads it once per run and shares the answer between the steps. A check run of a repository with 838 tags
  makes 32 GitHub requests instead of 115, one of them for the tags instead of 84. The package doc of
  `pkg/reposetup/reconcile` states what a run costs in requests and which steps share which read (#2254).

- The repositories schema devctl ships (`pkg/reposetup/schema/repositories.schema.json`) is regenerated from
  giantswarm/github's `.github/repositories.schema.json` at commit 3a5bbec: it gains `agentMerge`, the repository's
  opt-out from agent merges (boolean, default `true`, with the live schema's description text), so `repo validate`
  accepts `agentMerge: false` without a token and refuses a non-boolean value, naming the field. `requiredChecks`
  stays in the shipped copy ahead of the live schema, which gains it with giantswarm/github#6199 (#2287).

- The circleci and release steps of `repo reconcile` run only for a repository with a CircleCI pipeline:
  `.circleci/config.yml` on its default branch, or `gen.ci.generate: true` in its entry (a generated pipeline, on a
  first creation not on the branch yet). Without one both steps read `skipped: no CircleCI pipeline`: a configuration
  repository or a repository released by GitHub Actions is not followed, gets no deploy key, and its release is not
  held against a pipeline; a project someone followed by hand is left as it is. The answer costs one request per run,
  shared by the two steps, and none when the entry declares the pipeline (#2269).

- The `customer` flavour is a profile of the repository set-up reconciler: a customer repository's branch protection
  and default branch are the customer's own flow and it has no CircleCI pipeline of ours, so the protection, circleci,
  codeowners and release steps are skipped on it (`skipped: flavour customer`) and the settings step never plans the
  rename of its default branch; settings, permissions, renovate, metadata, lifecycle and catalog run as declared
  (#2272).

- `reconcile.DefaultBaseline` has strict status checks off (`StrictChecks: false`): a branch need not be up to date
  to merge, so on a repository with Renovate and sweep traffic a merge no longer invalidates every other open pull
  request and re-runs its CI. The protection step plans `strict checks true → false` where a repository has them on
  and reports both values of every protection change (`enforce admins false → true`). `EnforceAdmins: true` is the
  company baseline, final (#2267).
- A finding kind carries whether it is advisory, and the finding carries it in the artifact (`"advisory": true`), so a
  reader knows its weight without knowing the kinds; `default-icon` is advisory. The run's `converged` is true when
  every step ended `ok`, `skipped` or `repaired`, or `reported` with advisory findings only: the default icon alone
  does not keep a repository from counting as set up as declared, while a finding a person must fix
  (`abs-prerequisite`, `renovate-not-scanned`, `red-release`, ...) now clears the mark it left untouched before.
  `repo reconcile` marks an advisory finding in its table and `repo status` in its findings lines (#2268).
- `repo reconcile` never rebuilds a tag. The latest release's tag without a pipeline is the finding
  `missed-tag-build` (a person's to fix, not advisory), the step reads `reported` and no request is written in any
  mode; the fix is the next tag, or the tag's pipeline triggered by hand. The triggered rebuild published a chart or
  an image nobody asked for. `red-release` is unchanged (#2270).
- The generated `renovate.json5` lists the marge sweep's App
  (`giantswarm-marge[bot]`) in `gitIgnoredAuthors` beside taylorbot. The sweep
  commits a bot PR's changelog entry onto the bot's own branch, and Renovate
  stops rebasing and autoclosing a branch whose last commit is by an author it
  does not ignore.
- `repo create` declares `align: true`: a repository created through the product is opted in to alignment by its
  creation, so the run that follows its merged pull request sets it up instead of only checking it. The entry's key
  order is name, description, visibility, componentType, align, gen (#2259).
- `repo create` is pull-request-last: the dry run, then the repository created with the person's GitHub login
  (description and visibility from the declaration), the scaffold pushed as the one commit on `main`, then the
  declaration's pull request in `giantswarm/github`, validated in existing mode for a repository that exists and is
  the author's. The output names the repository, the scaffold commit and the pull request (text and
  `--output json`); `--dry-run` prints the plan and writes nothing. The organisation does not let members create
  repositories: the caller's role is read before the first write and anyone but an owner is refused with the way
  out (`reconcile.NotOwnerRefusal`, `IsNotOwner`); a 403 on the creation gives the same text. A run interrupted
  after the creation resumes: a repository of the declared name the caller administers is continued (scaffold
  pushed when missing, the open pull request reported), anyone else's stays a refusal
  (`Entry.RefusedForTakenName`, `Remote.FindPullRequest`). `reconcile.Runner.Create` runs the create and scaffold
  steps standalone for one accepted entry with any authenticated client — the same steps `Run` executes for the
  reconciler — and returns the repository URL, the scaffold commit and the step results; giantswarm-repo-manager
  imports it to create as the person (giantswarm/giantswarm#37726, #2238).

### Removed

- `repo reconcile --enforce-admins`: the branch protection binds administrators too, the baseline's value with no
  knob; the flag and its "until the baseline decides" note are gone (#2267).
- `circleciclient.Client.TriggerPipeline` and `TriggerRequest`: the reconciler's tag rebuild was their only caller
  (#2270).

### Fixed

- `pr wait` exited 4, `required_missing`, at the timeout when a required context was absent although a run of the
  head was still pending, a fork's workflow run awaiting a maintainer's approval among them, whose approval is what
  reports the contexts. The timeout is now exit 2 whenever anything is still pending, with `unfinished` naming the
  run and the absent contexts; exit 4 is a fail-fast at the poll that sees every check, status, run and workflow
  of the head finished with a required context still absent, without waiting for the timeout. The e2e scenario
  `fork-awaiting-approval` requires contexts and asserts 2; the new `required-never-reported` asserts 4 before the
  timeout; `docs/pr-wait.md` states the precedence (#2313).

- A one-commit pull request squash-merged under its commit's own subject rather than the title the title check had
  accepted, because GitHub's default names the squash commit `COMMIT_OR_PR_TITLE`; an unconventional subject is then
  neither released nor listed by auto-release (git-cliff's `filter_unconventional`), and the next merge's release notes
  omit the pull request. The repository baseline of `repo reconcile`, `repo setup` and `repo status` now names the squash
  commit after the pull request's title (`squash_merge_commit_title: PR_TITLE`), read with the repository or through
  GraphQL like the six merge settings and repaired with them; the auto-release decide step names an unconventional
  subject in the unreleased range as a workflow warning, where before the run only counted zero deciding commits.
  Every declared repository carries GitHub's default today, so one that is not opted in to alignment reads
  `settings drift: squash_merge_commit_title COMMIT_OR_PR_TITLE → PR_TITLE` on its next check until its team opts it
  in (the nightly repairs the opted-in entries) or an administrator sets it by hand.

- `auth login` ran the GitHub device flow with a placeholder client id and GitHub refused it; the binary now carries the
  client id of the `giantswarm-devctl` GitHub App (`Iv23liWio5REm4MfY2Mw`, owned by the `giantswarm` organization),
  and `docs/auth.md` names the App (#2276).

- `repo status`, `repo checks`, `repo reconcile` and the reconcile engine's settings step as an identity without admin rights on
  the repository (an App installation with `administration: read`, a member with read access) reported `allow_squash_merge`,
  `allow_update_branch`, `allow_auto_merge` and `delete_branch_on_merge` as `false → true` drift on every repository:
  `GET /repos/{owner}/{repo}` carries the six merge settings for admins only and an absent field read as `false`. The step
  reads them through GraphQL (`Repository { mergeCommitAllowed squashMergeAllowed … }`, which answers any identity that
  reads the repository) when the repository came without them, and reports them as an `unchecked` finding — never as
  drift — when that read fails too. An admin identity costs no extra request.

- `reconcile.Refused` (the result of an entry the schema refuses: `devctl repo reconcile`, the reconciler's
  artifact, giantswarm-repo-manager's check) reports `converged: false`: nothing was checked against the
  declaration, so the repository is not set up as declared, and not drifted either -- the fix is in the entry.
  `Result.Refused()` tells a refusal from drift; the table header and `devctl repo status` say "entry refused"
  instead of "drift or failed steps". Before, a refused entry read as converged next to its refusal.

- `pkg/authstore` on Linux speaks the Secret Service API over D-Bus itself instead of through go-keyring: the default
  collection is resolved through `ReadAlias` and unlocked only when its `Locked` property says so (a prompt is
  completed through `org.freedesktop.Secret.Prompt`), items are searched, created (replacing the record of the same
  identity), read and deleted on the resolved collection. oo7-daemon and KeePassXC, which refuse `Unlock` on the alias
  path, now hold devctl's tokens; GNOME Keyring and KWallet keep working with the records written before. A failing
  call reads `keychain <call> on <collection path> (<Secret Service process>): <error>`. macOS and Windows keep
  go-keyring's backends (#2304).

- `repo create` and the set-up engine scaffold a repository whose flavours produce a chart (`app`, `cluster-app`)
  from a template without one -- the Go service with the `app` flavour -- with the chart of `giantswarm/template-app`
  at `helm/<name>` (and `.abs/main.yaml` pointing at it), the name substituted and the team annotation set, so the
  first release's chart job builds instead of failing on a chart that is not there. The dry run, the plan and the
  scaffold commit name the chart (`chart` on the entry and the scaffold in `--output json`).
- `repo validate`: the embedded repositories schema admits as `gen.flavours` exactly the flavours devctl's generators
  accept -- `helmchart` is gone from the enum (it is a `gen precommit` flavour, the schema's `gen.preCommit`), so a
  declaration naming it is refused at the schema, in existing mode too, instead of by the first `devctl gen` run
  align-files makes for the repository; a test pins the enum to `gen.AllFlavours()`. The creation rules no longer add
  a second problem to a field the schema refused. The embedded copy is otherwise level with `giantswarm/github` main
  again (field descriptions). (giantswarm/github#6122)
- `repo reconcile`: the `renovate` step no longer reports `renovate-not-scanned` on a repository younger than a day
  that has a configuration but no trace of a run: Renovate's first run is not due yet (the hosted App picks a new
  repository up within hours, the Dependency Dashboard issue follows), so the step is `ok` with a summary saying so,
  and a repository created by the same run counts as young. An older repository without a trace stays a finding
  (giantswarm/giantswarm#37726).
- `repo reconcile`: the `renovate` step no longer fails on a private repository whose issues the token cannot read —
  a GitHub App without `issues: read` is answered 403 there (and, on a public repository, a list of pull requests
  only, without the Dependency Dashboard issue). The refused read falls through to the commit trace on the default
  branch; without one the step reports the `unchecked` finding naming the permission to grant, never `failed`, and
  the `renovate-not-scanned` fix names the permission the dashboard's visibility takes (#2251).
- `repo reconcile`: the `catalog` step dispatches for a private repository. The reconciler runs the step as its
  workflow run, whose token holds the `actions: write` the App does not, and that token sees public repositories
  only: the run's repository lookup answered 404 and every step of the invocation ended
  `skipped — repository does not exist`, so the catalog regeneration was never dispatched and the step's
  `in the catalog` verdict was out of reach for a private repository. `--dispatch-token-envvar` names the token
  for the step's two workflow-run calls (listing the runs, dispatching) alone; every read -- the repository
  lookup, the catalog, the mapping -- stays with the GitHub token, which sees the repository.
  `reconcile.Runner.Dispatch` is the client behind the flag; nil keeps everything on `GitHub`
  (giantswarm/giantswarm#37726, #2244).
- `gen workflows` (`--release-workflow=auto-release`): the "Verify CircleCI picked up the tag" step passes with a
  `::warning::` annotation when CircleCI does not follow the project (HTTP 404 with a token) instead of failing the
  run. The first tag of a repository created pull-request-last is pushed before the repository set-up reconciler
  follows the project on CircleCI, and the reconciler reports the missed tag build, so every new repository's first
  Auto Release run was red although the release existed. Following takes an administrator's token the workflow does
  not hold. A followed project whose pipeline is missing after the wait is
  still triggered by the step (HTTP 200, empty list); API errors still fail it (giantswarm/giantswarm#37726, #2245).
- `repo reconcile`: the `renovate` step decides from the repository's own evidence — a Renovate configuration
  file and a trace of a run (the Dependency Dashboard issue, else a pull request or a commit of Renovate's) —
  instead of the Renovate installation's repository list, which only an organization owner's token can read:
  under the reconciler's GitHub App token the step was skipped on every run. Both present is `ok`, as is a
  configuration that disables Renovate; what is missing is the `renovate-not-scanned` finding (replacing
  `renovate-missing`) with the fix — the installation covers all repositories, so a missing configuration is
  added, and a configuration without a trace means Renovate has not run yet or refuses it. The installation's
  list is read as detail for the summary when the token can. The step waits for the scaffold on an empty
  repository and is skipped on a repository archived on GitHub; `repo setup --renovate=false` leaves the step
  out (giantswarm/giantswarm#37726, #2247).
- `repo reconcile`: the `release` step triggers the missed tag build whenever the latest release's tag has no
  CircleCI pipeline. It skipped the trigger when the most recent pipeline was newer than the release, taking that
  as a sign the tag's pipeline was merely further down the list — but a repository created pull-request-last is
  followed on CircleCI after its first tag, and the follow builds the default branch right away, so the first
  release stayed unbuilt with `pipeline not among the 1 most recent, not verified`. The step now decides from the
  tag alone: it pages through the project's pipelines until the tag's pipeline is found, until a pipeline older
  than the release is seen (the tag's would have been listed before it) or until the pages end, and triggers only
  when none exists — never twice. `circleciclient.ListPipelines` reads one page at a time (`PipelinePage`,
  `NextPageToken`) (giantswarm/giantswarm#37726, #2243).
- `repo reconcile`: `lifecycle: archived` unfollows on CircleCI once. The step read the v2 project to decide
  "followed", which answers 200 for ever — unfollowed or stopped alike (checked live) — so every run planned and
  re-applied `unfollow on CircleCI` on every archived repository. It now reads the token user's follow from the v1.1
  project settings (the state the unfollow changes), unfollows and stops the project building
  (`circleciclient.StopBuilding`, the UI's "Stop building"), and verifies the follow is gone before calling the
  repair done; a second run is `ok` (giantswarm/giantswarm#37726, #2231). `circleciclient.Following` reads the state.
- `repo reconcile`: an entry the validator refuses is a result, not exit 2 without output — one step `entry`,
  verdict `reported`, one finding per problem (`gen-circleci-refused` for `gen.ci.generate`, the new `entry-refused`
  otherwise) with the field to fix, exit 0: the declaration is at fault, not the run; a flag or token error still
  exits 2. The reconciler workflow of giantswarm/github ran one invocation per declared repository, and 26 of
  Bumblebee's 72 jobs died on the empty result (giantswarm/giantswarm#37726, #2229). `reconcile.Refused` builds the
  result. `--enforce-admins` (default true, the baseline's) is the documented knob for whether the branch protection
  binds administrators, for the reconciler to pass once giantswarm/giantswarm#36733 decides.
- `pkg/githubclient.ReportedChecks`, `repo reconcile`, `repo checks`: on a repository whose every commit on the
  default branch is tagged — a fresh repository whose only commit is the scaffold, tagged v0.1.0 by auto-release
  within seconds — the head is the candidate commit instead of a not-found error, so the checks that reported on it
  (`pre-commit`, the pipeline's jobs) are required on the first run. The protection step's `unchecked` finding is
  worded by cause: a 401/403 names the permissions to grant (`statuses: read`, `checks: read`), a branch without a
  commit is "nothing reported yet" without a finding (giantswarm/giantswarm#37726, #2228).
- `repo reconcile`: the `catalog` step maps by chart, not by repository name: the component's
  `giantswarm.io/helmcharts` annotation in `catalog/components.yaml` names the charts to look for in the
  apps-to-teams mapping, matched by chart name (`chartName` overrides and `-app` suffixes differ from the
  repository), private-registry charts left out; a component without a public chart — a Go service without a chart,
  a library, a CLI — ends the step `ok` ("in the catalog; no public chart to map") instead of dispatching the
  mapping run on every reconcile for a repair that cannot converge (giantswarm/giantswarm#37726, #2227).
- `repo reconcile`: the `circleci` step grants the CircleCI token's GitHub user `admin` on the repository before
  following the project and revokes the grant right after, when that user is not an administrator already — CircleCI
  follows a project for a repository administrator only ("only a project's Github administrator may setup Circle"),
  and the reconciler's identity holds push through the bots team, so the first live run on two fresh repositories
  failed at the follow and left the scaffold's v0.1.0 tag unbuilt (giantswarm/giantswarm#37726, #2226). The grant and
  the follow are two changes of the step (`--dry-run` plans both); `pkg/circleciclient` gains `Me` (`GET /api/v2/me`).

- `pkg/reposetup`, `repo validate`, `repo status`, `repo reconcile`: the creation rules (`gen.flavours`/`gen.language`
  set, the chart-name convention, generated CI has a job, the name free on GitHub) apply to entries being added
  only; an existing entry is valid if the schema accepts it (giantswarm/giantswarm#37726, #2213). The Validator
  takes `Request.Mode` -- `ModeCreate` (the default, as before) or `ModeExisting` (schema alone; the name check's
  verdict is reported, never refuses -- a missing repository is the reconciler's finding; no review guard notices)
  -- and `Result.Mode` says which ran. `repo validate --mode create|existing` defaults to `create` with `--entry`
  and to `existing` for a whole file, so the validation check on giantswarm/github, which names the added entries,
  runs as before. `repo status` and `repo reconcile` validate in existing mode (`reconcile --added` in create
  mode). Before, 222 of the 443 entries declared in the real team files were refused and the read-mode checks of
  giantswarm-repo-manager could not run over them; in existing mode none is.

- `pkg/gen/input`: devctl builds as a module dependency again. The template provenance files (`*.template.sha`, written by `go generate`, gitignored) were embedded by name, so `pkg/reposetup` — which renders scaffolds with the gen inputs since v8.60.0 — could not compile from the module proxy (`pattern x.template.sha: no matching files found`). Each site now embeds `<template>*` and reads the `.sha` through `input.TemplateSHA`, which falls back to the module version's tree link when the file is absent; generated output is unchanged where `go generate` ran.

### Added

- `repo create` and `repo status`, the laptop's client of the repository set-up engine
  (giantswarm/giantswarm#37726, #2215). `repo create --team … --name … --component-type … --flavour … --language …
  --description … --visibility …` renders the declaration as an entry of the team's file in giantswarm/github
  (`gen.ci.generate` as the CircleCI generator decides), placed alphabetically with the rest of the file kept byte
  for byte, validates it through the engine (schema, creation rules, the name on GitHub), prints the dry run and
  opens the team-file pull request as the person with their own token (`$GITHUB_TOKEN` or the gh CLI's login) --
  a taken name or a wrong flavour is refused before a pull request exists, and the guard notices say beforehand
  whether the machine approves the change or the team reviews it (membership read from GitHub as the person). It
  never creates a repository or touches settings. `repo status [owner/]repo` prints the set-up state -- every step
  with its verdict -- from giantswarm-repo-manager through a muster endpoint (`--muster-endpoint`,
  `$MUSTER_ENDPOINT`) when reachable, else from the engine's checks in read mode with the person's tokens.
  Documented in `docs/repo.md`.
- `pkg/reposetup`: `Creation` renders a declaration from fields, `InsertEntry` places it in a team file's
  text, `Remote` reads team files and memberships from giantswarm/github and opens the pull request as the
  caller, `CreationPullRequest` shapes it; `pkg/reposetup/manager` is the client of giantswarm-repo-manager's
  `get_repository` tool over MCP's streamable HTTP transport.
- `repo reconcile REPOSITORY` (giantswarm/giantswarm#37726, #2214): runs the repository set-up steps of
  `pkg/reposetup/reconcile` locally as the person — the way to repair a repository when the reconciler
  workflow is down and to develop the engine against a real repository. The desired state is the
  repository's entry of a giantswarm/github team file (`--team-file`, validated as the reconciler validates
  it; the scaffold is rendered from it on an empty repository) or, for a repository without a declaration,
  `--team` alone. `--dry-run` prints what a repair would change; `--steps` restricts the run; `--added`
  allows the create step; the result is a table, or the structured value with `--output json`. The CircleCI
  steps read the token from `$CIRCLECI_TOKEN` (`--circleci-token-envvar`) and are skipped without one.
- `pkg/reposetup/reconcile`: `Result.WriteTable` renders a run for a person, `Result.Failed` lists the
  steps that could not run; `Request.Pipeline` hands the protection step just-generated pipeline documents
  instead of the repository's `.circleci`; a run restricted to steps that do not read the team needs no
  team. `pkg/reposetup.UndeclaredEntry` is the accepted entry of a repository without a team-file
  declaration.

### Fixed

- The repository set-up engine pushes the scaffold as a conventional commit, `feat: initial scaffold of <name> from
  <template>`, so the generated auto-release workflow tags the created repository `v0.1.0` from it: git-cliff drops a
  non-conventional commit (`filter_unconventional`), and with the old `Scaffold <name> from <template>` subject a new
  repository never got a release and the first-release check could not pass. The CODEOWNERS pull request's commit
  follows the same rule (#2214).

### Changed

- `repo setup` and `repo checks` run the set-up engine's steps instead of their own GitHub calls
  (giantswarm/giantswarm#37726, #2214). Required checks follow the reported-only rule: a context is required
  once it has reported on the default branch or a recently merged pull request, and a required context
  nothing reports any more is removed — `repo setup` no longer requires the contexts of whatever ran on the
  default branch so far (the `create-release / …`, `update-go_modules-graph` and `ci/circleci: setup` ghosts
  of a fresh repository cannot recur), and `repo checks` removes ghosts without being told. `repo checks`
  without `--update` prints the drift; the CircleCI pipeline's branch-side jobs are read from the
  repository's `.circleci` when `--circleci-dir` is not given; the release workflows, `update-go_modules-graph`,
  `aliyun`, `validate-changelog` and `check-values-schema` are never required. `repo setup --renovate` checks
  that the Renovate installation covers the repository and reports a missing one with the fix instead of
  failing on the `PUT` an organization owner alone may make. Both commands print the run's result as a table
  (`--output json` for the structured value); a step that could not run to its end is the non-zero exit.

- `pkg/reposetup/reconcile`: the repository set-up steps as check and repair, idempotent — create (only from an added entry), scaffold push before protection, settings baseline, team permissions, branch protection with required checks on the reported-only rule (ghost contexts removed, contexts following the generated pipeline), CircleCI follow, setup workflows and checkout key, webhooks, Renovate installation (check only), CODEOWNERS (a pull request), description and visibility, lifecycle `archived` (archive and unfollow), catalog and mapping (the giantswarm/github workflows), first-release verification (a missed tag build is triggered). `reconcile.Runner.Run` returns a structured `reconcile.Result`; a redirect on the declared name is followed as a rename, and what is not repaired (repository gone, `gen circleci` refusal, ABS prerequisites, red release, default icon) is reported with the fix. Table-tested against in-process fakes of GitHub's and CircleCI's REST surfaces.
- `pkg/circleciclient`: a CircleCI client for follow and unfollow (v1.1), the project, its settings, checkout keys, pipelines, workflows and jobs (v2).
- `pkg/githubclient`: `Config.BaseURL` points the client at another GitHub API host.

- `gen circleci`: `--component-type template --team TEAM` renders a template repository's chart before it builds
  (giantswarm/giantswarm#37726, #2217). A template's chart lives at `helm/{APP-NAME}` and carries the
  placeholders a repository created from it fills in (`{APP-NAME}`, `{TEAM-NAME}`, `{APP HELM REPOSITORY}`),
  so the generated `build-chart` of `giantswarm/template-app` was red on every pipeline. For
  `componentType: template` the chart job is now an inline job on the app-build-suite executor that renders
  the checkout with fixture values (`sample-app`, the owning team from the team file, an example Helm
  repository) and runs app-build-suite on the rendered chart, so green means a repository created from the
  template passes its first chart build. Nothing is released from a template: no chart-test job, no push
  jobs, no release leg, no `tests/ats` files, and the job runs on `main` too. A template without a chart
  and every other component type render the pipeline as before.
- `repo validate` and the `pkg/reposetup` package, the front half of the repository set-up engine
  (giantswarm/giantswarm#37726, #2213): an entry of a giantswarm/github team file is validated against
  the repositories schema — fetched from `giantswarm/github` main, with an embedded copy that already
  carries the plan's `description`, `visibility` and `lifecycle: archived` fields as the fallback — and
  against the rules for a repository the reconciler creates: `gen.flavours` and `gen.language` are
  mandatory, `gen.ci.generate` defaults to `true` (written into the rendered entry), the name is
  lowercase and free on GitHub (an existing repository or a redirect from a renamed one is taken), a
  chart repository is named after its chart (no `-app` suffix, `gen.ci.chartName` equal to the name),
  and `language: node` is refused until the Node template exists. Every refusal names the field. The
  template is derived, never declared: Go → `giantswarm/template`, chart-only (`generic` with the `app`
  flavour) → `giantswarm/template-app`, customer, configuration, python and kyverno-policy → the minimal
  scaffold. The command prints the dry run as JSON on stdout (log lines go to stderr) — the rendered
  entry, the implied template, the name verdict, the problems and the guard notices: an author outside
  the owning team and team-planeteers keeps the team's review, more than three added entries get a
  person — and exits non-zero on a refusal. The package is the one place validation and rendering live
  for the reconciler workflow, `repo create` and giantswarm-repo-manager; the scaffold rendering is the
  engine's next half.
- `gen workflows`: the `auto-release` flow can now cut release candidates. A pull request titled
  `feat-rc:` or `fix-rc:` marks its change as part of a candidate, and the workflow tags
  `vX.Y.Z-rc.N` instead of `vX.Y.Z`, flagged as a GitHub pre-release. The decision is taken over
  every unreleased commit: a candidate is tagged when at least one of them carries `-rc` and no
  unreleased `feat`, `fix` or breaking commit does not, so an unmarked `chore(deps)` from Renovate
  cannot end a candidate cycle and an unmarked `feat` or `fix` closes it at the stable version the
  candidates were leading to. A commit counts as breaking through either spelling, a `!` in the
  subject or a `BREAKING CHANGE:`/`BREAKING-CHANGE:` footer. A push that carries nothing releasable
  tags no candidate, so a `docs`- or `style`-only push behaves inside a cycle the way it does
  outside one. `zz_generated.auto_release.yaml` also gains a `workflow_dispatch` trigger with a
  `release-type` input to close a cycle when no pull request is left to merge.
- `gen workflows`: `zz_generated.semantic_pull_request.yaml` passes `types` and `header_pattern` to
  `giantswarm/github-workflows`, so `feat-rc` and `fix-rc` pass the PR title check. The action's
  stock parser reads the type with `\w*` and cannot match a hyphen, so the `header_pattern`
  override is what admits the type at all. The titles are accepted in every repository but only
  act under `--release-workflow=auto-release`. `security` joins the accepted types, which the
  action's default list never held although `cliff.toml` maps it to a Security changelog group.

- `pkg/reposetup`: scaffold rendering, the back half of the repository set-up engine
  (giantswarm/giantswarm#37726, #2213). `Renderer.Render` renders an accepted entry into a directory: the
  template's tree (tarball of `giantswarm/template` or `giantswarm/template-app` main, or a `TemplateSource`
  of the caller's) with its placeholders replaced (`REPOSITORY_NAME`, `{APP-NAME}` in paths and files,
  `{TEAM-NAME}` as the chart's team annotation, `{APP HELM REPOSITORY}`), CODEOWNERS as align-files writes
  it, the minimal scaffold (README, LICENSE, DCO, SECURITY.md, CODEOWNERS, `.gitignore`) for configuration,
  customer, python and kyverno-policy repositories, and the generated files — through the same `devctl gen
  makefile|workflows|llm|precommit|circleci|renovate` commands align-files runs, in its order and with its
  flags, so the first align run after creation changes nothing (`Scaffold.Commands` lists them). The
  chart-only template offers the vendir sync and patch-script scaffolding of `devctl app bootstrap` as
  `Entry.Options` of the dry run, chosen through `RenderRequest.Options`. Golden trees for every kind of the
  derivation (Node deferred with its template) and a test that runs the generators a second time over each
  scaffold and asserts no change.
- `repo validate` refuses `gen.ci.generate: true` for a declaration the CircleCI generator has no job for
  (a language other than go or node, no app flavour, no `gen.ci.image.dockerfile`): align-files' `devctl
  gen circleci` would fail on the created repository. The field is `gen.ci.generate`.
### Changed

- `gen precommit`: `devctl gen precommit` now writes `helm/<chart>/values.schema.json` itself,
  in-process (generate via `helm-values-schema-json`, fix `$ref`+`additionalProperties: false` ->
  `unevaluatedProperties: false`, normalize via `schemalint`), instead of leaving it to the
  generated pre-commit hook. The hook is now read-only: it reproduces the same pipeline to a
  scratch file and fails on a diff against the committed file, but never rewrites it, so it can no
  longer fight `schemalint-verify` or itself over key ordering (giantswarm/giantswarm#37267). Its
  failure message points at `devctl gen precommit` as the fix. Every `schemalint` and
  `helm-values-schema-json` version in the generated config -- the hook's
  `additional_dependencies` and the `schemalint-verify` hook's `rev:` -- is now read from devctl's
  own `go.mod` at build time, so it is the single source of truth instead of a hardcoded literal
  in the template. The normalizing binary and the verifying one cannot land on different versions
  any more. Output is unchanged at the current pin. See giantswarm/devctl#2195.

  This moves one network call from pre-commit into `devctl gen precommit`. A chart whose
  `values.yaml` uses the `$ref: $k8s/...` alias makes the generator fetch the Kubernetes JSON
  schema from `raw.githubusercontent.com` and bundle it, so `gen precommit` fails for that chart
  while the host is unreachable. It names the URL it could not read. Charts without the alias
  generate offline, as before.
- `gen`: an `input.Input` that sets both `Generate` and `TemplateBody` is now rejected with an
  `invalidInputError` instead of silently running `Generate` and ignoring the template. The two
  fields are two ways to produce the same file, and which one won was only an accident of the order
  of the branches in `internal.Execute`.
- `gen precommit`: `--k8s-schema-version` now reaches `k8sSchemaVersion` as well as `k8sSchemaURL`.
  Both the generated `helm/<chart>/.schema.yaml` and the Go config behind
  `helm/<chart>/values.schema.json` carried a hardcoded `v1.33.1` in the version field while the
  URL followed the flag, so a repository on another Kubernetes version described itself with two
  different versions. Output is unchanged for the default version.

- `gen circleci`: the generated chart-test jobs (`execute-chart-tests` and, with `--ats-on-release`,
  `execute-chart-tests-release`) let the repository shape and size the kind cluster they test on
  (devctl#2188, architect-orb#928):
  - A kind `Cluster` configuration at `.ats/kind-config.yaml` is passed to both jobs as `kind_config`
    (architect-orb 10.5.0), derived from the file's presence the way the image pipeline is derived from a
    `Dockerfile` and the Node version from `.nvmrc` -- no `gen.ci` key. The cluster's shape (feature gates,
    runtime config, kubeadm or containerd patches, extra nodes) is test content that changes with
    `.ats/main.yaml` and the tests, so it lives next to them and one repository edits one place. The job
    keeps naming the cluster and choosing the node image. Without the file the jobs render as before. First
    use: the kagent API v2 chart smokes, whose runtime (Agent Substrate) needs the `ClusterTrustBundle`,
    `ClusterTrustBundleProjection` and `PodCertificateRequest` gates and `certificates.k8s.io/v1beta1` on
    the cluster -- gates that are fixed at `kind create` and that app-test-suite 1.x, which provisions no
    cluster, cannot set.
  - `--ats-resource-class <class>` renders `resource_class` on both jobs (`medium`, `large`, `xlarge`,
    `2xlarge`, the orb job's enum; a class outside it, or the flag on a repo without chart-test jobs, is
    rejected at generation time). Unset renders nothing and the orb default `medium` applies. Deliberately
    separate from `--resource-class`, which sizes the cli `go-build` and the Node job: a chart smoke that runs
    a real workload on the job's kind cluster has nothing in common with a cross-compile. Surfaced as
    `gen.ci.atsResourceClass` in giantswarm/github.
  - `gen circleci --help` and the comment above the generated job name both conventions. New golden
    `agent.ats-kind-config.workflows.yml`; `go test ./pkg/gen/input/circleci/ -update` now rewrites the golden
    files from the current template instead of each test carrying its own compare-and-print block.

### Changed

- `gen circleci`: the architect orb pin moves to `10.5.0`, which adds the `run-tests-with-ats` `kind_config`
  parameter the generated chart-test jobs now set (architect-orb#929). Golden workflows regenerated.
- `gen circleci`: the canonical app-test-suite (ATS) test stack moves to `pytest==9.0.3` and
  `pytest-helm-charts==1.3.5` (`uv.lock` re-resolved). pytest 9.0.3 carries the fix for GHSA-6w46-j5rx-g56g
  (CVE-2025-71176), which Dependabot flags on the generated `tests/ats/pyproject.toml` of every chart repo;
  pytest-helm-charts 1.3.5 is the first PyPI release that allows pytest 9 (`pytest>=9.0.2,<10`). The
  generator tests no longer hardcode the stack's versions -- they assert the pins exist, that both layouts
  (Pipfile and pyproject.toml) agree and that `uv.lock` locks them -- so the grouped Renovate bumps of this
  stack stop failing `go-build`.
- `gen circleci`: the architect orb pin moves to `10.4.1`, which ships the `sync-china-registry` fix
  (architect-orb#921): the job now waits until the image the push job published is visible from the
  in-China runner, child manifests included, before `regctl image copy` starts. The runner reads the
  Southeast Asia replica of `gsoci.azurecr.io`, which receives a pushed image asynchronously, and the old
  fixed 10 × 5 s retry never fit a multi-GiB image (every `vllm` tag since v0.4.7 needed a manual rerun).
  No template change; the generated jobs take the orb's new `replica-wait-minutes` default of 60. Golden
  workflows regenerated.

### Fixed

- `gen workflows`: the `auto-release` flow applies its "nothing releasable" check to a forced
  `release-type: rc` as well. A candidate extends an open cycle, and once the stable release has closed
  the cycle there is none to extend: the run now skips and says so, the way `release-type: stable`
  already did, instead of tagging `vX.Y.Z-rc.N` above the shipped `vX.Y.Z`.
- `gen workflows`: the `auto-release` flow renders the release notes after it decides which tag to cut, so
  the "Full Changelog" compare link on a release candidate points at the tag that was created
  (`compare/v0.1.5...v0.1.6-rc.1`) instead of at the stable target, which has no tag until the cycle closes.
- `gen workflows`: `cliff.toml` counts only `vX.Y.Z` tags as releases (`tag_pattern`, replacing the
  `ignore_tags` pre-release filter), so a `vX.Y.Z-rc.N` candidate tag no longer ends the range the
  `auto-release` flow releases from. The version and the notes span the whole candidate cycle: the stable
  release that closes a cycle carries every commit the candidates carried (devctl#2202), a
  `workflow_dispatch` run with a candidate tag on `HEAD` names the target of the cycle rather than the last
  stable version (devctl#2201), and a candidate pushed during a cycle keeps the target the cycle
  established. Candidate notes become cumulative as a result: `rc.2` lists what `rc.1` listed, plus what is
  new. The `decide` step's describe baseline also excludes build-metadata tags (`--exclude='*+*'`), so both
  ends of the flow read the same set of tags.
- `gen makefile`: the `app` flavour's targets (`helm-docs`, `lint-chart`, `update-chart`, `update-deps`) work
  on repositories that also have the `go` flavour. The root `Makefile` includes `Makefile.*.mk` in name order,
  so `Makefile.gen.app.mk` is parsed before `Makefile.gen.go.mk` sets `APPLICATION` from the Go module; the
  `check-env` guard was a parse-time `ifndef` and `DEPS` a parse-time `:=`, so every go+app repository
  (vm-manager, model-manager) failed with `Makefile.gen.app.mk:47: *** APPLICATION is not defined` although the
  variable is set once make runs a recipe. The guard is now a recipe line and `DEPS` is recursively expanded, so
  both read `APPLICATION` at recipe time; the per-dependency `$(DEPS)` targets, whose names were also fixed at
  parse time, become a loop inside `update-deps`. App-only repositories keep working as before: `APPLICATION`
  from `Makefile.custom.mk` or the command line is honoured, and an unset `APPLICATION` still fails the target
  with a message (and `make help` no longer trips over it).
- `gen precommit`: the `helm-schema-<chart>` hook now installs and pins its own generator
  (`github.com/losisin/helm-values-schema-json/v2@v2.6.0` in `additional_dependencies`, next to the
  existing `schemalint` pin) and calls that binary directly, instead of calling a bare `helm schema`
  resolved from the developer's global helm plugin dir. The old guard only checked that *a* plugin was
  installed, never which version, so a dev machine on a different version silently rewrote the committed
  `values.schema.json` (v2.3.1 vs v2.6.0 is 40 lines in `hello-world-app`) and reported `Passed` while CI
  then rejected it. The pin now lives in exactly one place: `HELM_VALUES_SCHEMA_JSON_VERSION` is gone from
  the generated pre-commit workflow (with its plugin cache and install steps), `helm_values_schema_json_version`
  is no longer passed to the reusable `sync-from-upstream` workflow, and the Renovate custom manager for
  `losisin/helm-values-schema-json` is replaced by the existing `go`-datasource manager on
  `additional_dependencies`. Regenerated chart repos need no helm plugin at all; the hook env costs ~16 s
  cold and ~0.3 s warm. With the last `helm` caller gone, the dead `HELM_VERSION` env var and its
  Renovate custom manager are removed too: nothing in the generated workflow installs helm, and
  `helm-docs` is a separate binary.
- `gen workflows`: the generated `sync_from_upstream.yaml` now passes `helm_docs_version` instead of
  letting the reusable `sync-from-upstream` workflow default it. A skew against the pins in
  `zz_generated.pre-commit.yaml` made every sync PR commit a chart README or `values.schema.json`
  built by the wrong tool version and then fail its own check. (The matching
  `helm_values_schema_json_version` pin added here is superseded by the `gen precommit` fix above,
  which removes that pin entirely; neither has shipped yet.)

### Added

- `gen workflows --helm-docs-regen` (app flavour) generates `zz_generated.helm-docs-regen.yaml`: on pull requests
  from `renovate/**` and `dependabot/**` branches it regenerates the chart README (helm-docs) and
  `values.schema.json` (the `helm-schema-<chart>` hooks) and pushes the result back onto the PR branch with the
  taylorbot PAT, so an image-tag or values-key bump no longer fails the `pre-commit` check on files only the hooks
  can rewrite (giantswarm/agent-platform#295, #301, #302, #320; agent-sandbox#38; agentgateway#3). The hooks and
  their tool pins are read from the repo's own `.pre-commit-config.yaml` at run time, every hook runs twice with
  the second pass required clean, a clean tree is a no-op so the run the push triggers exits without pushing again,
  and the job is skipped with a warning where the secret is not available (fork and Dependabot-triggered runs).
  Opt in through `gen.helmDocsRegen: true` in giantswarm/github. Closes #2185.
- `gen renovate` lists `dev@giantswarm.io` in `gitIgnoredAuthors`: the author the generated workflows commit
  with (helm-docs-regen, update-chart, sync-from-upstream) now counts as Renovate's own, so a branch they pushed
  to keeps being rebased and autoclosed instead of retitled "- abandoned".
- `version update` installs a release binary only after its cosign Sigstore bundle verifies. Every devctl release
  asset comes with a `<asset>.bundle` next to it: cosign's keyless signature made by the CircleCI pipeline and
  recorded in Rekor. The download is verified against that bundle for a CircleCI build of
  `github.com/giantswarm/devctl` (Sigstore public-good trust root, fetched through TUF and cached under
  `~/.sigstore/root`) before anything is written: a release without a bundle is refused before the download, a
  download that does not match its signature before the write, and the installed binary stays untouched either
  way. Version lookups (`version check`, and the check that runs before every command) do not look at the bundle,
  so an unsigned release can never block devctl; cache, exit status 125 and `DEVCTL_UNSAFE_FORCE_VERSION` behave
  as before.
- `gen circleci --image-resource-class <platform>=<class>` (repeatable): overrides the CircleCI resource class
  of the native per-architecture `build-image` jobs for one platform, on both the branch and the release leg.
  Defaults stay linux/amd64 on `small` and linux/arm64 on `arm.medium`. For an image whose leg is dominated by
  exporting, compressing and SBOM-scanning a very large result rather than by the build itself: vllm's 22 GB
  arm64-only image spent 36 of its 37 release-leg minutes there on the 2-vCPU `arm.medium`, after the build
  proper had taken 8. The class must belong to the platform's architecture (the orb refuses a mismatch instead
  of emulating) and the platform must be in `--image-platforms`; both are checked at generation time. Requires
  `--image-native-builds`.
- `gen circleci`: the chart-test jobs (`execute-chart-tests`, `execute-chart-tests-release`) run with the
  architect context, and the architect orb pin moves to 10.4.0. The orb's `run-tests-with-ats` turns the
  context's registry credentials into `/var/lib/kubelet/config.json` on the kind cluster it creates, so a chart
  whose image lives in gsociprivate.azurecr.io can be smoke-tested without `imagePullSecrets` (first users:
  alfred-app, mcp-runbooks). Without the context the orb step only prints a notice.
- `repo checks --update --circleci-dir <repo>/.circleci`: reconciles the required `ci/circleci: <job>` contexts
  with the pipeline itself. The branch-side jobs of the generated `workflows.yml` and the repo-owned `custom.yml`
  (every workflow; a job counts unless its branch filter has `only:` or ignores every branch) are required once
  they have reported, exactly like `--checks-if-reported`, and every required `ci/circleci:` context whose job the
  pipeline no longer has is removed. Until now the align-files action computed the jobs itself and could only
  add: when `gen.ci.branchPublish` renamed the branch image job from `build-image` to `push-to-registries`, the
  stale `ci/circleci: build-image` requirement stayed on model-manager and tunnelport and blocked every pull
  request with a check nothing could report. A pipeline that cannot be read leaves the CircleCI contexts as they
  are; contexts of other systems (GitHub Actions workflows) are never touched.

### Changed

- `pkg/updater` moves from the unmaintained `rhysd/go-github-selfupdate` to `creativeprojects/go-selfupdate`,
  which the shared validator `github.com/giantswarm/selfupdate-cosign` plugs into. Same GitHub API calls, same
  asset selection (`devctl-<os>-<arch>`), same in-place replacement of the running binary. The one fallback that
  goes away: without a token in the environment the old library also read `github.token` from the git config;
  the new one calls the GitHub API anonymously in that case, which works for the public devctl repository.
- `gen circleci`: the default app-test-suite tag moves to `1.0.3`, which waits for the bootstrapped CRDs
  (`--cluster-crds`) to be `Established` before the Helm deploy. On 1.0.2 a chart whose templates render a
  kind from those CRDs (giantswarm/agent renders a kagent `Agent`) failed `helm upgrade --install` with
  `no matches for kind`, because `kubectl apply` returns before the API server serves the new kinds.

### Fixed

- `repo checks --update --checks-if-reported`: when the reported checks cannot be read, the names are skipped for
  this run with a warning instead of failing the command. The discovery reads commit statuses and check runs,
  which a GitHub App token can only do on a private repository when the App holds the "Commit statuses" and
  "Checks" read permissions (`403 Resource not accessible by integration` otherwise); the align-files App did
  not, so the first alignment with `gen.ci.requireCircleCIChecks` aborted on the three private repositories
  (muster-runbooks, agent-platform-ui, web-assets) before syncing any file. `--checks` and `--remove` are applied
  as before in that case.

### Added

- `repo checks --update --checks-if-reported <names>`: adds a required status check only when that context
  has reported on the default branch's latest non-tag commit or on the head of one of the three most recently
  merged pull requests (the `repo setup` discovery, reused). Names that have not reported yet are logged and
  skipped, so a generated CircleCI job that never ran (no CircleCI project, first alignment after the job
  appeared) cannot become a required check nothing can satisfy and block every PR. giantswarm/github's
  align-files uses it to make the generated pipeline's branch-side jobs (`ci/circleci: build-chart`,
  `ci/circleci: execute-chart-tests`, `ci/circleci: go-build`, …) required checks on repos that set
  `gen.ci.requireCircleCIChecks`; today only the GitHub-Actions checks are required, so GitHub auto-merge
  (align PRs, Renovate platform automerge) merges before CircleCI reports or past a red chart build
  (tunnelport#80 released v1.2.6 without a chart). `--checks` keeps adding unconditionally; `strict` and
  checks not named stay untouched as before.

### Fixed

- `gen circleci`: the default app-test-suite tag moves to `1.0.2`, which accepts `oci://` values for
  `upgrade-tests-app-catalog-url` at config validation (1.0.0 and 1.0.1 rejected them as `Wrong catalog URL`
  although the resolver supports OCI catalogs), so upgrade scenarios can take their stable chart from
  `oci://gsoci.azurecr.io/charts/giantswarm`.

### Fixed

- `gen circleci`: the default app-test-suite tag moves to `1.0.1`. The `1.0.0` image ships a CRD bundle whose
  `gateway-api.yaml` starts with helm's `Pulled:`/`Digest:` lines (captured from stdout while syncing), so
  `kubectl apply --server-side -f /etc/ats/crds` rejects the directory and every 1.0.0 run fails at CRD
  bootstrap before a single test runs. 1.0.1 fixes the bundle and guards it with a unit test.

### Added

- `gen circleci`: new `--go-test-artifacts <dir>` flag (`gen.ci.go.testArtifacts` in giantswarm/github).
  Renders `post-steps` on the generated `architect/go-build` job that keep a directory `make test` writes
  (e.g. muster's `test-reports/`: the integration suite's per-scenario JSON with the complete instance
  logs, which the console shows only a trimmed tail of) as a CircleCI build artifact when the job fails:
  staged `when: on_fail`, uploaded with `store_artifacts`, so a green run stores nothing. The append-only
  custom.yml merge cannot add post-steps to a generated job (yq `*+` appends a second `architect/go-build`
  entry, which CircleCI rejects), so the generator carries it. Go repos only; the path must be a relative
  directory under the checkout.
- `gen workflows --release-workflow auto-release`: after creating the release, the generated
  `zz_generated.auto_release.yaml` verifies that CircleCI picked up the tag (repos with a
  `.circleci/config.yml` only): it polls the v2 pipeline list for up to 120 s and, if no pipeline appears,
  triggers one through the API with the org secret `CIRCLECI_API_TOKEN`. Without the secret the step is a
  detector (a missing pipeline on a public project fails the run with the manual `curl`; a 404 on a private
  project is a warning). GitHub delivers the tag-push webhook once and never retries; on 2026-09-04 CircleCI
  answered three of ~55 tag pushes with an empty HTTP 400 and those releases had no pipeline.
- `gen circleci`: new `--go-build-path` flag setting the architect `go-build` job's `path` param for
  Go repos.
- `gen circleci`: new `--ats-on-release` flag (`gen.ci.atsOnRelease` in giantswarm/github). Restores
  the pre-v8.45.0 chart pipeline: an `execute-chart-tests-release` job on the tag, after the release
  image, with `push-chart-release` gating on it. Mutually exclusive with `--skip-ats`.
- `gen circleci`: new `--ats-version` flag (`gen.ci.atsVersion` in giantswarm/github). Pins the
  app-test-suite container tag on both `run-tests-with-ats` jobs (`app-test-suite_container_tag`). A
  1.x tag also emits `create_kind_cluster: true` on both jobs -- app-test-suite 1.x no longer
  provisions clusters, the job creates the kind cluster and hands over its kubeconfig (the architect
  orb's `run-tests-with-ats` `create_kind_cluster` opt-in) -- and switches the generated test
  dependency file from `tests/ats/Pipfile` (pipenv, ATS <= 0.15) to `tests/ats/pyproject.toml` +
  `uv.lock` (uv, ATS 1.x), deleting the Pipfile. Empty, the default, changes nothing; a 0.x tag pins
  the tag on the legacy `dats.sh` path. The rest of a repo's migration (`.ats/main.yaml` without the
  `*-cluster-type` keys, tests that install with Helm instead of an App CR) is the repo's own; see the
  app-test-suite CHANGELOG. Mutually exclusive with `--skip-ats`. The canonical uv layout is embedded
  next to the Pipfile and Renovate bumps both layouts in the one `ATS test dependencies` PR.
- The architect orb pin moves to `10.3.0`, which ships `run-tests-with-ats`'s `create_kind_cluster`
  (architect-orb#917): the job installs kind, creates the cluster and hands its kubeconfig to
  app-test-suite 1.x, which provisions none. `--ats-version` with a 1.x tag emits that parameter, and
  10.2.0 rejects it as unknown, so the pin and the knob ship together.
- `gen circleci`: new `--image-native-builds` flag (`gen.ci.image.nativeBuilds` in giantswarm/github),
  the opt-in for native per-architecture image builds. It emits one `architect/build-image` job per
  platform in `--image-platforms` (default `linux/amd64,linux/arm64`), each on a resource class of that
  architecture (`small` for amd64, `arm.medium` for arm64), and switches the generated
  `push-to-registries` / `push-to-registries-release` jobs to `merge-digests: true`, so they join the
  per-architecture digests into the tagged index instead of building. Nothing is emulated and the
  builds run concurrently, so wall clock is the slower single native build: on `giantswarm/backstage`
  the image path went from 14m16s to 3m22s. Job names downstream (`push-to-registries`,
  `push-to-registries-release`, `sync-china-registry`) are unchanged; the validate-only branch job
  `build-image` becomes `build-image-<arch>` jobs, so a `custom.yml` that requires `build-image` must
  follow when a repo opts in. A platform with no native class is rejected at generation time. Off by
  default: the generated output for every repo that does not set it is unchanged. Pays off for
  Dockerfiles with real work in `RUN` steps; a `COPY` of a cross-compiled binary gains nothing.
- The architect orb pin moves to `10.2.0`, which ships `build-image` and `merge-digests`.
- `gen circleci`: new `--ats-branch-only` flag (`gen.ci.atsBranchOnly` in giantswarm/github). The chart
  pipeline keeps `execute-chart-tests` on branches and the canonical `tests/ats/Pipfile`, but no longer
  generates the tag-time `execute-chart-tests-release`; `push-chart-release` gates directly on
  `build-chart`. The tag is cut from the merge commit of a PR whose `execute-chart-tests` already passed
  on that tree, so the tag-time run re-tested an identical tree: on `agent-platform-standalone` it was
  4m10s of a 5m57s release (p50 over 53 tag pipelines since 2026-08-31) and its only three failures were
  the apptestctl bootstrap race, never a chart regression. It is only sound when the repo's branch
  protection makes the `ci/circleci` statuses required checks on the default branch and requires
  branches to be up to date with it (strict); set that first. Mutually exclusive with `--skip-ats`,
  which drops both jobs and the Pipfile. Off by default: the generated output for every repo that does
  not set it is unchanged.

### Changed

- `gen circleci`: **app-test-suite 1.0.0 is the default** for every generated-CI chart repo
  (`DefaultATSVersion`, next to the orb pin). Unset, `--ats-version` now renders exactly what
  `--ats-version 1.0.0` renders: `app-test-suite_container_tag: "1.0.0"` and `create_kind_cluster: true`
  on the chart-test jobs, and `tests/ats/pyproject.toml` + `uv.lock` instead of the Pipfile. The repo owns
  the rest of its migration (`.ats/main.yaml` without the `*-cluster-type` keys, tests that install with
  Helm instead of an App CR); a repo that has not migrated yet pins a 0.x tag (`--ats-version 0.15.0`,
  `gen.ci.atsVersion: "0.15.0"` in giantswarm/github) to stay on the legacy dats.sh path with the Pipfile.
  `--skip-ats` no longer conflicts with the tag: the opt-out wins and the tag is ignored.
- `gen circleci`: the chart tests run on branches only. The generated chart pipeline no longer carries
  `execute-chart-tests-release`; `push-chart-release` gates directly on `build-chart` (plus the release
  image when there is one), so a release is the build + push alone. The tag is cut from the merge commit
  of a PR whose `execute-chart-tests` already ran on that tree; on `agent-platform-standalone` the
  tag-time re-run was ~4m10 of a ~6 min release, never caught a chart regression and failed only on the
  apptestctl bootstrap race. Every app-flavour repo changes shape at its next alignment. For the tag to
  be the tree the PR tested, make the `ci/circleci` statuses required checks on the default branch with
  up-to-date branches (strict); a repo that cannot, or whose `custom.yml` jobs require
  `execute-chart-tests-release` (backstage's second catalog push did), sets `--ats-on-release`.

- `gen workflows --release-workflow auto-release`: `cliff.toml` skips `docs` commits the way it skips
  `style`. git-cliff bumps at least the patch for every commit that is not skipped, so a docs-only
  merge tagged and published an identical artifact (agent-platform-standalone v0.35.2 and v0.35.3,
  both CLAUDE.md-only); now such a push computes the current version and the workflow logs
  `No releasable commits since vX.Y.Z; skipping tag.` The template comment that claimed the bump
  ignores the parser groups is replaced by the actual rule. `chore` and `ci` keep releasing.
- `gen workflows`: run the generated "Fix Go vulnerabilities" (nancy-fixer) workflow every Wednesday night.

- `gen circleci`: whether the chart's `appVersion` is stamped now follows the repo's shape. A repo with
  **no image pipeline** gets `override_app_version: false` on its chart jobs, so app-build-suite keeps the
  `appVersion` declared in `Chart.yaml`. The chart version is still always stamped.

  `appVersion` is the version of the packaged application. A repo that builds its own image ships the app
  it packages, so `appVersion` equals the chart version and stamping it is correct. A chart-only repo
  packages an app built elsewhere, so its `appVersion` is that app's version. Of the organisation's 249
  chart repos, 97 chart-only repos declare a real upstream `appVersion` that packaging overwrote, while 65
  of the 82 image-building repos already declare `appVersion` == `version`.

  Derived from the same signal as the image pipeline (`HasDockerfile`, or a non-empty `ImageDockerfile`),
  so no repo has to opt in.

- `gen circleci --override-chart-app-version`: overrules that derivation in either direction. Leave it
  unset for the derived behaviour; pass `=false` for a repo that builds an image and still declares a
  foreign `appVersion`, or `=true` for a chart-only repo that wants its `appVersion` stamped anyway.

- align helm-docs command in `Makefile.gen.app.mk.template` with the `pre-commit-config.yaml.template` configuration

### Deprecated

- `gen circleci --ats-branch-only` (added in v8.43.0): branch-only chart tests are the default now. The
  flag is accepted and ignored; `gen.ci.atsBranchOnly` in giantswarm/github is ignored the same way.

- `gen circleci --keep-chart-app-version` (added in v8.39.0): use `--override-chart-app-version=false`. It
  is still honoured, and the repos it was added for no longer need either flag — the derivation covers
  them.

### Fixed

- `gen makefile` (Go flavour): the `nancy` target scans `go list -json -deps ./...` again instead of
  `go list -json -m all`, so it reports vulnerabilities only in packages actually compiled into the
  module. `-m all` walks the whole module graph and flags modules that are never built: on
  `team-stamper` it reported 7 vulnerable modules of which 5 are absent from the build, including
  `golang.org/x/crypto` and the 13 CVEs attributed to it. #680 adopted `-deps ./...` for precisely
  this reason in 2023; #1964 reverted to `-m all` in June only to work around nancy's 10 MB stdin
  cap, which nancy had introduced days earlier in v2.0.0 and then made configurable, defaulting to
  100 MB, in [v2.1.0](https://github.com/sonatype-nexus-community/nancy/releases/tag/v2.1.0) six
  days later. The cap that motivated the workaround is therefore gone: `cluster-standup-teardown`,
  the repository that hit it, emits 12 MB, and CI already runs nancy 2.1.0. The target's doc comment
  now says v2.1.0 rather than v1.0.37.

### Security

- `app bootstrap`: `--name` and `--team` are now validated as identifiers. `--team` is joined into
  `repositories/team-<team>.yaml` inside the `giantswarm/github` checkout, so a value carrying `..`
  or `/` reached a file outside that directory; `--name` becomes an argument of `devctl repo setup`,
  so a value starting with `-` was read as a flag. The bootstrap flow also runs only `devctl`, `git`
  and `vendir`, checked against an allow list before the subprocess starts.
- `deploy`: `--app-name`, `--app-catalog`, `--target-namespace`, `--management-cluster`,
  `--organization` and `--workload-cluster` are now validated as identifiers, and `--app-version` as
  a version. All seven are passed to `kubectl gs gitops add app`, where a value starting with `-`
  was read as a kubectl flag.
- `pkg/appstatus`: `WaitForAppDeployment` validates the app name, organization namespace and
  management cluster it passes to `tsh` and `kubectl`, rather than trust its callers.
- `release create`: the provider name is validated before it is joined into the releases directory,
  so it cannot address a directory outside it. The chart name and version read from a release
  manifest are validated before they are interpolated into the `raw.githubusercontent.com` URL the
  cluster dependency lookup fetches.

### Fixed

- `pr`: the parent command reports an error from `--help` instead of discarding it.
- `gen precommit`: new `--go-generate` flag renders a `go generate ./...` step into
  `zz_generated.pre-commit.yaml` before the hooks, and devctl sets it for itself. golangci-lint
  compiles the packages it analyses, and devctl embeds 37 gitignored `*.template.sha` provenance
  files, so the job stopped at a load error instead of linting. The flag is opt-in and requires
  `--language go`: the job installs no code generators, so a repository whose directives need
  `controller-gen` or `mockgen` must not get the step.
- `release`: `getLatestGithubRelease` and the Kubernetes release lookup name the upstream
  repository through `kubernetesGitHubOwner`/`kubernetesGitHubRepo` rather than repeat a literal
  that also means the component name.

### Changed

- `release bumpall`: reads `slices` from the standard library instead of `golang.org/x/exp/slices`,
  which is deprecated. `golang.org/x/exp` is dropped from `go.mod`.
- The `github.Ptr` and `github.String` helpers, deprecated in go-github v92, are replaced by the
  `new` builtin.
- Repeated string literals are named: template data keys and delimiters in the `workflows`,
  `precommit` and `makefile` generators, and provider names, release types, output formats and
  component names in `pkg/release`.
- Permissive file and directory modes in tests are tightened to `0600` and `0750`.
- `.golangci.yml` sets `goconst.ignore-tests`. A table test repeats a fixture across its cases so
  that the input and the expectation can be read together. It also excludes `fmt.Fprint`,
  `fmt.Fprintf` and `fmt.Fprintln` from errcheck: the runners print to an injected `io.Writer`,
  and a failed write to the user's terminal cannot be reported to the user's terminal.

### Removed

- `app bootstrap`. Its replacement is `repo create`: the declaration goes through the engine's validation and
  the team-file pull request instead of a hard-coded `-app` suffix, an unvalidated team-file write, an SSH clone
  and a push-then-protect sequence; the reconciler creates the repository from the merged entry. The vendir and
  kustomize sync and the patch-script scaffolding live on as options of the chart-only template's dry run
  (`pkg/reposetup` options). The `app` command group is gone with its only subcommand.

### Fixed

- `pr wait` and `pr merge` no longer wait for a CircleCI pipeline of a repository CircleCI does not build. CircleCI's
  project lookup answers a project for every repository the token's user sees on GitHub, set up on CircleCI or not,
  so a head carrying `.circleci/config.yml` in a repository never set up there -- a template repository whose
  configuration is for the repositories created from it -- was waited for until the timeout
  (`unfinished: circleci pipeline for <sha> (absent)`). CircleCI is now part of the verdict only when the project has
  run at least one pipeline; a project without one is judged from GitHub alone with a warning in the document, like a
  repository without a project.

## [8.23.0] - 2026-06-24

### Changed

- devctl now dogfoods its own generated CircleCI + auto-release standard. The repo-owned
  `Makefile.zzz.custom.mk` makes `make test` depend on `generate-go`, so the gitignored
  `*.template.sha` provenance files (consumed via `//go:embed`) are regenerated before tests
  and the cross-compile run. This lets the generated `go-build` job (`test_target: test`) build
  devctl from a clean CI checkout and also fixes clean local `make test`.

## [8.22.1] - 2026-06-24

### Changed

- `gen makefile`: the generated `make test` target is now cgo-adaptive. It runs `go test … -race ./...`
  wherever a C toolchain is available (laptops, coding agents, GitHub Actions, any cgo-capable runner)
  and degrades to cgo-free `go test … ./...` where it is not. This unblocks the make-target CI interface
  (8.22.0's `test_target: test`): the architect `go-build`/`go-test` image has no C compiler and runs
  with `CGO_ENABLED=0`, so a hardcoded `-race` made `make test` fail with `-race requires cgo` on every
  generated-CircleCI Go repo. The single `make test` command is now correct in every environment — CI
  runs it verbatim and local runs keep race detection — so it stays the one relocation target for the
  hand-written `ci.yaml` removal workstream.

## [8.22.0] - 2026-06-24

### Changed

- `gen circleci`: the make-target CI command interface. The generated `go-build` job now sets
  `test_target: test`, so the architect orb runs the repo's `make test` instead of its hardcoded
  `go test … ./...`; CI and local runs converge on one command. The generated `make test`
  (`gen makefile`) is unchanged (`go test -ldflags … -race ./...`), so CI runs the same test command
  developers already run locally. A repo that wants a different CI test command (e.g. drop `-race`, add
  an integration suite, `govulncheck`, `helm-unittest`, or a CRD-freshness check) overrides its own
  `make test`. This is the relocation target for the hand-written `ci.yaml` removal and the make-target
  interface from the make-target-ci-interface ADR (no architect-orb change — the orb already exposes
  `test_target`). Reaches repos via the next align-files run.

## [8.21.1] - 2026-06-23

### Changed

- `gen circleci`: for `cli`-flavour Go repos (the six-arch cross-compile that attaches binaries to the
  GitHub Release), the generated `go-build` job now sets `build_concurrency: auto` and
  `resource_class: large`. The GOCACHE persistence from architect-orb #838 (orb v9.5.0+) makes warm
  builds fast, but the cross-compile is still cold after every `go.sum` bump; compiling the six
  architectures concurrently on a larger box instead of sequentially on 2 vCPUs removes the remaining
  critical-path cost. Service (non-cli) repos are unchanged. No signing or nancy changes. Reaches repos
  via the next align-files run.

## [8.21.0] - 2026-06-22

### Fixed

- `release create`: stop fetching the Flatcar releases JSON manifest during release-notes generation. The
  manifest was downloaded and then discarded for Flatcar, and the request had no timeout, so a slow
  `flatcar.org` would hang `devctl` even when the Flatcar version was pinned via `--component flatcar@<version>`.

### Changed

- `release create`: the changelog HTTP fetch now uses a 30s timeout instead of relying on the default client.
- `release create`: the Flatcar releases JSON URL and channel are now configurable via the `FLATCAR_RELEASES_URL`
  and `FLATCAR_CHANNEL` environment variables (defaulting to the stable channel).

## [8.20.5] - 2026-06-21

### Changed

- `gen circleci`: bumped the pinned `giantswarm/architect` orb from `9.5.1` to `9.5.2`. v9.5.1's oversized-SBOM
  fallback used `cosign attest --tlog-upload=false`, which cosign v3 rejects (`--tlog-upload=false is not
  supported with --signing-config`), so large-SBOM releases (e.g. `vllm`) still failed at the attest step.
  v9.5.2 opts out of the transparency log the cosign-v3 way: it re-attests through a signing config with Rekor
  removed but the TSA kept (`--signing-config <file> --new-bundle-format`), so the SBOM attestation keeps a
  trusted timestamp (no Rekor body-size limit) and stays attached as an OCI referrer. Reaches repos via the
  next align-files run.

## [8.20.4] - 2026-06-21

### Changed

- `gen circleci`: bumped the pinned `giantswarm/architect` orb from `9.4.3` to `9.5.1`. v9.5.1 makes the
  cosign SBOM attestation degrade gracefully when its transparency-log upload fails persistently (a
  multi-MB SPDX predicate — e.g. the `vllm` CUDA image — overruns the public Rekor gateway and returns
  `502`): the orb re-attests with `--tlog-upload=false` so the signed SBOM stays attached as an OCI
  referrer (without a public Rekor entry) instead of failing the whole release. Image signing stays
  strict and the normal sub-limit path keeps its Rekor entry. Reaches repos via the next align-files run.

## [8.20.3] - 2026-06-20

### Changed

- `gen circleci`: bumped the pinned `giantswarm/architect` orb from `9.4.1` to `9.4.3`. v9.4.3 makes
  nancy tolerate the new Sonatype Guide API error (`guide API request failed`) the same way it already
  skips `error accessing OSS Index`, so a Guide outage or credit exhaustion no longer fails `go-build`
  (and the dependent `push-to-*` jobs). Reaches repos via the next align-files run.

## [8.20.2] - 2026-06-18

### Added

- `gen renovate`: the generated `renovate.json5` now sets `assignAutomerge: true`
  automatically whenever `reviewers` are configured. By default Renovate does not
  request reviewers on PRs it auto-merges; this ensures the configured reviewer is
  still requested for review on auto-merged Renovate PRs.

## [8.20.1] - 2026-06-17

### Fixed

- `gen workflows`: the auto-release `cliff.toml` now scopes `git-cliff --bump`'s
  baseline to the current branch (`use_branch_tags = true`) and ignores
  pre-release tags (`ignore_tags`). Previously `--bump` picked the globally
  highest-semver tag in the repo regardless of reachability, which (a) broke
  backports — a fix on `release-2.x` would bump from the latest `main` tag and
  tag e.g. `v3.1.1` on the 2.x line instead of `v2.8.1` — and (b) let a stray
  pre-release tag on an unmerged side branch (e.g. `v1.2.9-dev.0`) become the
  baseline for `main`, so a real `feat:` produced `v1.2.9-dev.1` instead of
  `v1.3.0`. Also corrects the misleading `topo_order` comment (it governs
  changelog section order, not the bump baseline).

## [8.20.0] - 2026-06-17

### Added

- `gen circleci`: new `--image-dockerfile` flag for repos whose Dockerfile is not at the repo root
  (e.g. `backstage` builds from `packages/backend/Dockerfile`). It sets the `dockerfile` param on the
  generated image build jobs (`build-image` and `push-to-registries-release`) and, because the image
  pipeline is otherwise derived from a root `os.Stat("Dockerfile")` that misses a nested Dockerfile, a
  non-empty value also turns the image pipeline on. The append-only `.circleci/custom.yml` merge cannot
  set the Dockerfile path on a generated job, so the generator carries it. Default off, so repos that do
  not set it get the identical config as before.

- `gen circleci`: the branch image-validation job (`build-image`, and the branch-publish
  `push-to-registries` when `--branch-publish` is set) now also gains the `--image-pre-build-job`
  `requires` entry. Previously only the release image (`push-to-registries-release`) waited on the
  pre-build job; the branch build compiles the same Dockerfile and needs the same workspace handoff, so
  it would otherwise fail building a Dockerfile that consumes pre-build artifacts. No change for repos
  that do not set `--image-pre-build-job`.

## [8.19.0] - 2026-06-17

### Added

- `gen circleci`: new `--chart-name` and `--force-public` flags for repos whose chart/registry shape
  does not fit the default. `--chart-name` overrides the chart name on every chart job (the
  `push-to-app-catalog` `chart` param and the `helm/<chart>` directory) for repos whose chart
  directory does not match the repo name (e.g. `docs-proxy` ships `helm/docs-proxy-app`).
  `--force-public` adds architect's `force-public: true` to the image (`push-to-registries`) and chart
  (`push-to-app-catalog`) release pushes for private repos that publish public artifacts (e.g.
  `web-assets`), which architect otherwise derives as private from the repo visibility; it is mutually
  exclusive with `--image-private-only`. The append-only `.circleci/custom.yml` merge cannot rename a
  generated job's chart or add `force-public` to it, so the generator carries both. Both default off,
  so repos that do not set them get the identical config as before.

## [8.18.0] - 2026-06-16

### Added

- `gen circleci`: new `--image-name` and `--image-platforms` flags for repos whose image pipeline
  does not fit the default shape. `--image-name` overrides the `giantswarm/<repo>` default image name
  on the generated image jobs (`push-to-registries` branch + release and `sync-china-registry`) for
  repos whose published image differs from the repo name (e.g. `kserve` publishes
  `giantswarm/kserve-controller`). `--image-platforms` overrides the buildx platform list on the
  `build-image` and `push-to-registries-release` jobs for single-architecture images (e.g. `vllm`
  ships `linux/arm64` only; an amd64 build has no prebuilt wheels and fails). The append-only
  `.circleci/custom.yml` merge cannot rename or re-platform a generated job, so the generator carries
  both. Both default off, so repos that do not set them get the identical config as before.

### Changed

- `gen workflows` / `gen precommit`: add the `merge_group` trigger to the generated GitHub Actions workflows so they also run for GitHub merge queues.

## [8.17.1] - 2026-06-16

### Fixed

- `gen workflows`: the generated `cliff.toml` now pins `[bump].initial_tag = "v0.1.0"`, so the first
  auto-release in a repo with no tags yet is tagged `v0.1.0` instead of git-cliff's default `0.1.0`.
  This keeps the inception release on the leading-`v` scheme the rest of the giantswarm stack expects
  (architect's `/^v.*/` tag filter, the workflow's `--match='v*.*.*'` describe, gitsemver). Once any
  `v*.*.*` tag exists, git-cliff already carries the prefix forward, so only the first release was
  affected.

## [8.17.0] - 2026-06-16

### Added

- `gen circleci`: new `--image-pre-build-job` and `--image-private-only` flags for repos whose
  image build does not fit the default shape. `--image-pre-build-job` adds a `requires` entry on the
  generated `push-to-registries-release` job for a repo-owned job defined in `.circleci/custom.yml`
  (e.g. a workspace-handoff pre-step that persists a file the Docker build context overlays) — a
  dependency the append-only `custom.yml` merge cannot inject into a generated job.
  `--image-private-only` ships the image to the private registry only (`gsociprivate`) via an
  explicit `registries-data`, replacing `split-china-push` and omitting the `sync-china-registry`
  job, so a private repo's image does not land in the public catalog. Both default off, so repos
  that do not set them get the identical config as before.

## [8.16.0] - 2026-06-16

### Added

- `gen circleci`: new `--app-catalog`/`--app-catalog-test` flags override the catalog the chart
  pipeline publishes to (the `push-to-app-catalog` `app_catalog`/`app_catalog_test` params). Empty
  defaults to `giantswarm-catalog`/`giantswarm-test-catalog`, so repos that do not set them get the
  identical config as before. Repos that ship to a different catalog (e.g. the internal
  `giantswarm-operations-platform`) set them so generation does not silently migrate their chart to
  the public catalog.
- `gen renovate`: support for an optional repo-owned `renovate-custom.json5`. When the file exists
  in the repo root at generation time, the generated `renovate.json5` references it as the last
  `extends` entry (`github>giantswarm/<repo>:renovate-custom.json5`), so repo-specific rules win
  over the shared presets. devctl never generates or touches the custom file, and because Renovate
  resolves `extends` from the default branch on every run, edits to it are live on merge without
  regeneration. A new optional `--repo-name` flag (defaulting to the working directory's basename)
  supplies the repo name for the preset reference. The generated file now also carries a DO-NOT-EDIT
  header pointing repo-specific rules at `renovate-custom.json5`.

## [8.15.2] - 2026-06-15

### Changed

- Releases: Disable minor version detection for Cluster Autoscaler.

## [8.15.1] - 2026-06-15

### Changed

- `gen renovate`: the generated `renovate.json5` now uses idiomatic JSON5 formatting -- unquoted keys, single-quoted values, and one array item per line with trailing commas -- matching the style of hand-maintained configs and the `renovate-presets`. This is a cosmetic change with no effect on Renovate's behavior; repos will see a one-time formatting diff on the next align-files sync.

## [8.15.0] - 2026-06-15

### Removed

- Releases: Remove description placeholder.

## [8.14.1] - 2026-06-11

### Changed

- Nancy: Use `go list -json -m all` for producing the dependency list.

## [8.14.0] - 2026-06-11

### Added

- `gen circleci`: new `--app-catalog`/`--app-catalog-test` flags override the catalog the chart
  pipeline publishes to (the `push-to-app-catalog` `app_catalog`/`app_catalog_test` params). Empty
  defaults to `giantswarm-catalog`/`giantswarm-test-catalog`, so repos that do not set them get the
  identical config as before. Repos that ship to a different catalog (e.g. the internal
  `giantswarm-operations-platform`) set them so generation does not silently migrate their chart to
  the public catalog.
- `gen renovate`: support for an optional repo-owned `renovate-custom.json5`. When the file exists
  in the repo root at generation time, the generated `renovate.json5` references it as the last
  `extends` entry (`github>giantswarm/<repo>:renovate-custom.json5`), so repo-specific rules win
  over the shared presets. devctl never generates or touches the custom file, and because Renovate
  resolves `extends` from the default branch on every run, edits to it are live on merge without
  regeneration. A new optional `--repo-name` flag (defaulting to the working directory's basename)
  supplies the repo name for the preset reference. The generated file now also carries a DO-NOT-EDIT
  header pointing repo-specific rules at `renovate-custom.json5`.

## [8.13.0] - 2026-06-11

### Added

- `gen renovate`: new `--reviewers`/`-r` flag bakes a top-level `reviewers` array into the generated `renovate.json5`. This is the single-source-of-truth path for repos whose config is regenerated by align-files (`devctl gen renovate`), where reviewers set by the subcommand below would otherwise be wiped on the next sync.
- `gen renovate reviewers`: new subcommand that sets the top-level `reviewers` array in an existing Renovate config (`renovate.json5` or `renovate.json`) in the current directory. It edits the file in place, replacing the `reviewers` value (or inserting the key if absent) while preserving all comments, key order and formatting, and matching the quote style the file already uses. Fails if no Renovate config is found. Intended for hand-maintained (non-generated) configs.

## [8.12.0] - 2026-06-11

### Added

- `gen circleci`: the default branch path now emits a `build-image` job (`push-to-registries` with
  `push: false`, new in architect orb 9.4.0) that validates the multi-arch image build on every
  branch without pushing anything. Dockerfile regressions now surface on the PR instead of at tag
  time. Repos with `branchPublish` keep their pushing branch job instead (it already exercises the
  Dockerfile); cli-flavour repos get the same two-linux-platform cap as the release push.

### Changed

- `gen precommit`: exclude the `.yarn/` directory from the `trailing-whitespace` and
  `end-of-file-fixer` hooks. Vendored yarn releases (`.yarn/releases/yarn-*.cjs`) must not be
  modified; yarn 4.15.0 ships with trailing whitespace in its bundle, which made the hooks fail.
- Auto-detect the `RepoName` used in the `gen precommit` command and make the `--repo-name` flag optional.
- The baked architect orb pin moved to 9.4.0 (adds the `push` parameter on `push-to-registries`).

## [8.11.1] - 2026-06-11

### Fixed

- `gen makefile`: the generated `make help` target now aligns target descriptions to the longest target name instead of a fixed 20-character column.
- `repo setup`: auto-detection now inspects the head commits of the 3 most recently merged PRs in addition to the latest non-tag commit on the default branch. Checks that only run on `pull_request` events (PR gatekeepers like `Heimdall - PR Gatekeeper`, CircleCI chart-package status checks that fire on PRs but not on push-to-main) never reported on the default-branch ref and were therefore silently dropped from the required-checks list on every `repo setup` re-run.
- `repo setup`: default `--checks-filter` now excludes `validate-changelog` and `check-values-schema` in addition to `aliyun`. `validate-changelog` is path-conditional (`on.pull_request.paths: CHANGELOG.md`) so auto-pinning it blocks PRs that don't touch that file. `check-values-schema` is managed explicitly by the align-files cycle (app-flavour repos only, with the correct `/ validate` suffix); auto-pinning the bare name produces the wrong required-check context.
- `repo checks --update --remove`: removing the last remaining required check no longer silently no-ops. Previously an empty check list was serialised with `omitempty`, so GitHub never received the field and left the requirement in place. The command now falls back to the `DELETE /required_status_checks` endpoint when the merged list is empty.

## [8.11.0] - 2026-06-10

### Added

- `gen workflows`: the generated `create_release_pr.yaml` now triggers on release-candidate branches (`<base>#release#{major-rc,minor-rc,patch-rc,rc,rc-release}`) for the `main`, `master`, and `release` bases, so RC releases get an automatic release PR just like the stable `major`/`minor`/`patch` tokens. Requires the matching support in [giantswarm/github-workflows#195](https://github.com/giantswarm/github-workflows/pull/195).

### Changed

- `gen circleci` now emits a dynamic-config pair instead of a single static config. `.circleci/config.yml` becomes a tiny repo-agnostic setup workflow (`circleci/continuation` orb, pinned like the architect orb), and the derived golden pipeline moves verbatim to `.circleci/workflows.yml`. At pipeline runtime the setup job deep-merges an optional repo-owned `.circleci/custom.yml` into `workflows.yml` (yq `*+`: maps merge, workflow job lists append) and continues with the result. Repo-specific jobs and workflows (e2e, nightly crons, mirrors) go in `custom.yml`, reference generated jobs by their bare names (`requires: [go-build]`), take effect on the PR that adds or edits them, and are never touched by devctl or align-files. A malformed `custom.yml` fails the setup job on the same PR. Known limitation (accepted): the merge appends -- a custom job cannot inject itself into a generated job's `requires`, so tag publishes are not gated on custom jobs; gate merges via GitHub required checks instead.

## [8.10.0] - 2026-06-10

### Changed

- `gen circleci`: bump the baked architect orb pin to 9.3.0 (additive `push-to-app-catalog` override params, cosign duplicate-Rekor-entry fix, gitsemver 2.0.1; no template shape change). Golden fixtures regenerated.

### Fixed

- Bump `golang.org/x/crypto` to v0.53.0 to resolve CVE-2026-46598 (`ssh/agent` ed25519 panic), which failed the nancy vulnerability check on every CI run.

## [8.9.0] - 2026-06-08

### Changed

- `gen workflows --release-workflow=auto-release` now writes `.github/workflows/zz_generated.auto_release.yaml` instead of the un-prefixed `auto-release.yaml`, bringing the file in line with the rest of the generated workflows (regenerable-via-`zz_generated.`-prefix instead of via `SkipRegenCheck`). Both the `auto-release` and `legacy` branches now also delete the legacy un-prefixed `auto-release.yaml`, so repos that adopted the flow before the rename are migrated automatically on next `devctl gen` run.

### Fixed

- `repo setup`: auto-detected required status checks now ignore check runs whose `conclusion` is `skipped` on the observed commit. Reusable release workflows (release-please's `create-release / Create release`, `auto-release`'s tag-only steps) emit skipped check_runs on every push to the default branch; previously these leaked into the required-checks list, producing entries that didn't reflect any real PR gate.

## [8.8.0] - 2026-06-08

### Added

- `gen workflows --release-workflow=auto-release` emits a push-based release flow: `.github/workflows/auto-release.yaml` + `cliff.toml` at repo root. On every push to `main` / a backport branch matching `release-[0-9]*` or `release-v[0-9]*` (so `release-2.x`, `release-v3.7.x` -- but not `release-notes` or other unrelated branches), the workflow runs `git-cliff --unreleased --bump --context` to compute the next semver from conventional commits since the latest reachable v*.*.* tag and `gh release create --target $GITHUB_SHA` to produce the tag + GitHub Release atomically. `cliff.toml`'s `[remote.github].repo` is auto-detected from the consuming repo's `origin` git remote URL. Mirrors the canonical version already deployed in muster, mcp-kubernetes, klaus-operator, agentic-platform, agentic-platform-mcps, agentic-platform-ui, mcp-toolkit, telemetrydeck-go, mcp-prometheus, klausctl, klaus-oci, mcp-debug, mcp-oauth (13 repos).
- `--release-workflow=auto-release` and `=legacy` are bidirectional: each branch emits the workflow files it owns and deletion inputs for the OTHER branch's files. Flipping the value (in either direction) leaves the repo with exactly one set of release files after the next gen run.

### Removed

- `gen workflows --release-workflow=release-please` (along with `--changelog-style` and `--auto-release-level`) no longer exists. `gen workflows` always emits the legacy `create-release-pr` / `create-release` / `validate-changelog` trio. The release-please opt-in is reverted (see giantswarm/github), and a push-based git-cliff alternative is planned to take its place later. The generator (`pkg/gen/input/workflows/internal/file/release_please*`) is deleted, together with the unused `Create*Deletion` / `ValidateChangelogDeletion` helpers that previously fed it.

## [8.7.0] - 2026-06-05

### Added

- `gen circleci`: Go repos that also carry the `cli` flavour now ship cross-platform binaries on their GitHub Release. `go-build` gets the six-platform `architectures` matrix, an `architect/upload-release-assets` job is added (tag-only) to attach the binaries, and the multi-arch release image push is capped to `linux/amd64,linux/arm64` so buildx does not try the darwin/windows targets under QEMU. Derived from the `cli` flavour -- no new flag or config. Non-cli Go repos (services, operators) are unaffected.

## [8.6.0] - 2026-06-05

### Added

- `gen renovate`: `--circleci-generated` flag. When set, the generated `renovate.json5` adds a `packageRules` entry that disables Renovate updates for the `giantswarm/architect` orb, because `.circleci/config.yml` (and the orb version it pins) is generated by `devctl gen circleci`. This stops Renovate from fighting align-files regeneration on circleci-generated repos.
- `gen circleci`: the generated `.circleci/config.yml` now carries a `DO NOT EDIT` header noting it is generated by `devctl gen circleci` and kept in sync by the giantswarm/github align-files workflow.

### Fixed

- `gen renovate`: the `--interval` schedule block produced invalid JSON5 (a stray leading comma after the `extends` array). It now renders a valid trailing-comma block.

## [8.5.0] - 2026-06-05

### Changed

- `gen circleci`: pinned architect orb bumped to `9.1.0` (adds signed SBOM attestations; no generated job/param shape change from `9.0.2`).
- `gen circleci`: remove `build-release-artifacts: true` from the generated `create_release` workflow for CLI-flavored repos. Binaries are now produced and signed by the architect-orb `upload-release-assets` path instead.
- Release binaries now include darwin/amd64, darwin/arm64, windows/amd64, and windows/arm64 alongside the existing linux targets. Windows binaries are named `devctl-windows-<arch>.exe`.

### Fixed

- Fix pre-commit config by adding a directive to install both pre-commit and commit-msg hooks

## [8.4.0] - 2026-06-03

### Added

- `gen circleci`: `--branch-publish` flag opts a repo into publishing a coupled amd64 dev image and dev chart on branch builds.

### Changed

- `gen circleci`: branch builds now run build + test only by default (`go-build`, `build-chart`, `execute-chart-tests`); image and chart pushes moved to the tag path. Job `name:` labels dropped the repo-name suffix for consistent labels (`go-build`, `push-to-registries`, `push-to-registries-release`, `sync-china-registry`, `build-chart`, `execute-chart-tests`, `execute-chart-tests-release`, `push-chart`, `push-chart-release`). Pinned architect orb bumped to `9.0.2`.

## [8.3.5] - 2026-06-03

### Changed

- Update `dispatch-update-chart-events` workflow permissions to allow reading commit datetime.

## [8.3.4] - 2026-06-02

### Fixed

- Release artifacts now build correctly; the CI workflow pins Go 1.26.3.

## [8.3.3] - 2026-06-02

### Fixed

- CI release artifacts now build correctly after the `gitsemver get` rename in v8.3.1; the CI workflow now pins gitsemver v2.0.0.

## [8.3.2] - 2026-06-02

### Fixed

- remove path filter from check-values-schema calling workflow

## [8.3.1] - 2026-06-02

### Changed

- `gen makefile` (`--language go`): the generated `Makefile.gen.go.mk` now resolves the build version with `gitsemver get` instead of `gitsemver version`. gitsemver [v2.0.0](https://github.com/giantswarm/gitsemver/releases/tag/v2.0.0) renamed the `version` subcommand to `get` (to avoid confusion with the `--version` flag), a breaking change: with gitsemver v2 the old `gitsemver version` invocation fails, leaving `VERSION` empty and breaking the build/package targets. Repos must have gitsemver v2.0.0+ on `PATH` after re-running `devctl gen makefile`.

### Fixed

- `gen workflows`: remove the `paths:` filter from the generated `check-values-schema` calling workflow. With the filter in place, PRs that do not touch Helm values files never triggered the workflow, leaving the `check-values-schema / validate` required status check permanently unsatisfied and blocking all merges. Without the filter the workflow fires on every PR; the reusable workflow iterates over `helm/*/Chart.yaml` globs that evaluate to empty on non-chart PRs, so the job exits 0 with no work done.
- `gen circleci`: the chart-test job (`architect/run-tests-with-ats`, "execute chart tests") now waits for the image push before deploying. It previously required only `build-<repo>-chart`, so on image-bearing chart repos it deployed the chart and tried to pull the freshly built dev image from gsoci before `push-to-registries` had finished, racing the push and flaking on `ImagePullBackOff` (the kubelet's pull backoff outlasts the test deadline once an early pull attempt fails). The job is now split into a branch variant requiring `push-to-registries` and a release variant (`execute chart tests on release`) requiring `push-to-registries-release`, each gated on `HasDockerfile` so chart-only repos are unaffected.

## [8.3.0] - 2026-06-01

### Changed

- `gen circleci`: **dropped the `--orb-version` flag**. The giantswarm/architect orb version is now baked into devctl as a constant (`OrbVersion`) next to the config template, with a Renovate custom manager keeping it current. The orb major and the template's required job/param shape are two halves of one compatibility contract; combining them at generation time via a passthrough let `architect@9.0.0` pair with a stale v8 template and emit silently-invalid config. Baking the version in means a major orb bump lands as a devctl PR → release → align-files pin bump, instead of a `giantswarm/github` literal that can skew against the template. Callers (`giantswarm/github` align-files) must stop passing `--orb-version`.

## [8.2.0] - 2026-06-01

### Changed

- `gen circleci`: rework the generated `.circleci/config.yml` for architect-orb **v9.0.0**. v9 removed the `multiarch:` parameter on `architect/push-to-registries` (the job always uses `docker buildx` now), so it is dropped from both the branch and the release image jobs. v9 also defaults `architect/go-build` to `linux/amd64,linux/arm64`; the previous branch-build `platforms: "linux/amd64"` optimization is dropped so branch builds validate the full multi-arch image (auto-derived from go-build's `.platforms`), matching the v9-idiomatic single-path model. The golden fixture and generator help text are updated to the v9 shape. No new fields or parameters.

## [8.1.0] - 2026-06-01

### Changed

- `gen workflows` (`--release-workflow=release-please`): generated `release-please.yaml` now passes `RELEASE_PLEASE_APPROVER_CLIENT_ID` and `RELEASE_PLEASE_APPROVER_PRIVATE_KEY` through to the reusable `release.yaml` workflow. These back the dedicated `release-please-approver` GitHub App that satisfies branch protection's required-approval rule on release-please PRs, so `--auto --squash` can complete the merge once required checks pass. The two new secrets are `required: false` upstream — repos that don't have the App installed or the org secrets configured see no behavior change. Requires the `release-please-approver` App to be installed with `Pull requests: Read and write` AND `Contents: Read and write` permissions (Contents: Read alone is silently disregarded by branch protection — empirically verified).

## [8.0.0] - 2026-06-01

### Added

- Test catalogs can now be populated when an app or component uses a dev version (`app-version-sha`) in its release creation.

### Changed

- Dev version detection now uses the git SHA suffix instead of the semver pre-release format, since app dev versions are typically identified by their appended git SHA.
- Changelog fetch is skipped for dev versions during release notes generation.

## [7.48.0] - 2026-05-31

### Added

- `gen circleci`: new generator that emits a standard `.circleci/config.yml` for the Go-service-with-Helm-chart use-case. The pipeline is derived from existing signals rather than a per-repo CI parameter block: `--language go` selects `architect/go-build`; a `Dockerfile` in the repo selects `architect/push-to-registries` (multiarch + split-china-push) plus the paired `architect/sync-china-registry`; the `app` flavour selects `architect/push-to-app-catalog` (app-build-suite executor) plus `architect/run-tests-with-ats`. Emits the aligned standard (orb pinned via `--orb-version`, default `8.3.0`; loose `/^v.*/` tags; branch builds amd64-only, tag builds multi-arch + publish chart). Configs with no applicable signal are rejected instead of rendering an empty `jobs:` list. `.circleci/config.yml` is registered as regenerable.

### Changed

- `gen workflows` (`--release-workflow=release-please`): also delete the legacy release workflow files (`.github/workflows/zz_generated.create_release.yaml`, `zz_generated.create_release_pr.yaml`, `zz_generated.validate_changelog.yaml`) on every run. A repo uses either the legacy `create-release` flow or release-please — never both. Previously devctl stopped generating the legacy files in release-please mode but left whatever was already on disk, so a migrating repo carried orphaned workflows that still triggered on the legacy branch/tag patterns. Uses the existing `input.Input{Delete: true}` primitive, so the change is a no-op for green-field release-please repos.

### Fixed

- `gen workflows` (`--release-workflow=release-please`): declare the root package in the generated `release-please-config.json`. The template now renders `"packages": {".": {}}` at the top level. `packages` is `required` per release-please's official `schemas/config.json`; without it release-please loads the config but has no package to release, logs `Found release tag with component '', but not configured in manifest` for every existing tag, and exits without opening a Release PR even when conventional commits exist. The top-level `release-type: "simple"` continues to apply as the per-package default. Pairs with the manifest seeding fix below — both are needed for a legacy → release-please migration to produce a Release PR on first run.
- `gen workflows` (`--release-workflow=release-please`): seed `.release-please-manifest.json` with the latest existing `v<major>.<minor>.<patch>` tag found in the target repo (`{".": "<latest>"}`) instead of always writing `{}`. Without this, repos migrating from the legacy `create_release` flow that already had release tags would get an empty manifest, causing release-please to log `Found release tag with component '', but not configured in manifest` and exit without opening a Release PR (no per-path baseline to compute "what changed since the last release" against). Green-field repos with no tags continue to get `{}`; pre-release tags and non-`v<major>.<minor>.<patch>` tags are ignored when picking the baseline; the file is still write-once, so an existing manifest is never overwritten.

## [7.47.0] - 2026-05-28

### Added

- `devctl repo checks --update` now accepts `--remove <names>` to drop required status checks. Combined with `--checks`, a single invocation can migrate a check from one name to another (subtract `--remove`, then union `--checks`). Existing checks not named in either flag are left untouched.

## [7.46.0] - 2026-05-28

### Added

- Release: Add ExternalDNS Crossplane Resources & RBAC Bootstrap.

## [7.45.0] - 2026-05-28

### Added

- Release: Add Cluster Autoscaler Crossplane Resources.

## [7.44.0] - 2026-05-28

### Added

- `gen workflows`: Add `--auto-release-level` flag (`none`, `patch`, `minor`, `major`; default `none`), only used with `--release-workflow=release-please`. It sets the `auto-merge-level` input of the `giantswarm/github-workflows` `release.yaml` reusable workflow, which auto-merges the Release Please PR once CI passes, up to the given bump level (`none` disables auto-merge). The consuming repo must have "Allow auto-merge" enabled and the `release-please` GitHub App on its branch-protection bypass list.

### Changed

- Generated `release-please.yaml` workflow now passes `RELEASE_PLEASE_CLIENT_ID` and `RELEASE_PLEASE_PRIVATE_KEY` secrets to the `giantswarm/github-workflows` `release.yaml` reusable workflow instead of `TAYLORBOT_GITHUB_ACTION`. This matches the App-based authentication in the reusable workflow (the reusable workflow feeds `RELEASE_PLEASE_CLIENT_ID` to `create-github-app-token`'s `client-id`). Requires those two org secrets to be available to the consuming repo.

## [7.43.0] - 2026-05-21

### Added

- `devctl gen workflows --release-workflow=release-please` now adds `pkg/project/project.go` to `extra-files`
  in `release-please-config.json` when the language is Go and the file exists, so release-please updates the
  version constant in the release PR. No post-release dev-bump PR is needed.
- `Makefile.gen.go.mk` now injects the version into the binary via `-X .../pkg/project.version=$(VERSION)` in
  `LDFLAGS`, alongside the existing `buildTimestamp` and `gitSHA` flags. `VERSION` is derived from
  `architect project version` (git tag), so local and CI builds self-report correctly without modifying
  `project.go` at build time.
- Version is created in the Makefiles by using `gitsemver version` instead of `architect project version`

### Fixed

- The generated `Release Please` workflow referenced a non-existent reusable workflow (`release-please.yaml`);
  corrected to `release.yaml`.
- `Makefile.gen.app.mk` used `$(APPLICATION)/charts` for Helm dependency paths; all repos place charts under
  `helm/$(APPLICATION)/`, so the `DEPS`, `update-deps`, `$(DEPS)`, and `helm-docs` targets now use
  `helm/$(APPLICATION)/` as the base path.

## [7.42.0] - 2026-05-21

### Added

- `devctl repo checks --update --checks <list> REPOSITORY` adds named checks to the required status checks on
  the default branch protection rule without touching any other branch protection or repo settings.
- Rename the generated `Values and schema` workflow name and its `check` job to `check-values-schema` for an
  unambiguous required-check reference in branch protection rules.
- `devctl repo setup` auto-detection now includes GitHub Actions check runs in addition to legacy commit
  statuses, so Actions-based checks are picked up as required checks.
- `devctl gen workflows --release-workflow=release-please` generates a Release Please workflow instead of the
  legacy `create-release-pr` / `create-release` / `validate-changelog` trio. `--changelog-style` controls the
  section headers: `legacy` maps commit types to `### Added/Changed/Fixed` (required by the
  `giantswarm/releases` changelog scraper); `release-please` uses the Angular preset. The Release Please
  config and manifest are written as scaffolding files (generate-once, not overwritten on subsequent runs).
- CHANGELOG scraper now parses `### Security` and `### Deprecated` sections from component changelogs. Output
  follows KaC canonical order: Added, Changed, Deprecated, Removed, Fixed, Security.
- Route the `security:` conventional commit type to `### Security` in the generated
  `release-please-config.json` (both `--changelog-style=legacy` and `--changelog-style=release-please`). Use
  `security:` (or `security(scope):`) for CVE fixes and vulnerability mitigations.

### Changed

- `devctl gen workflows --changelog-style=release-please` now writes the full Keep a Changelog mapping: `feat`
  to `### Added`, `fix` to `### Fixed`, `security` to `### Security`, and the remaining Angular types (`perf`,
  `revert`, `refactor`, `docs`, `style`, `test`, `build`, `ci`, `chore`) to `### Changed`.

### Changed

- `repo setup`, `repo setup renovate`: log a past-tense confirmation after the Renovate installation step.
  `repo setup --dry-run` logs `[dry-run] would add ...`.

### Fixed

- `repo setup --dry-run`: now actually skips GitHub mutations; the `DryRun` flag was never forwarded into the
  GitHub client.

## [7.41.1] - 2026-05-20

### Changed

- Change the PR name looked for in `devctl pr approve-align-files` from `Align files` to
  `chore: align files according to platform standards`

## [7.41.0] - 2026-05-20

### Added

- Generate a `semantic_pull_request.yaml` GitHub Actions workflow in every repo. It calls the new
  `giantswarm/github-workflows/.github/workflows/semantic-pull-request.yaml` reusable workflow, which
  validates that the PR title follows Conventional Commits. The check runs on `pull_request` (`opened`,
  `edited`, `synchronize`) and uses the action's default type set (`build`, `chore`, `ci`, `docs`, `feat`,
  `fix`, `perf`, `refactor`, `revert`, `style`, `test`).
- Add `compilerla/conventional-pre-commit` hook to the generated `.pre-commit-config.yaml`, gated to the
  `commit-msg` stage. Contributors enable it locally with `pre-commit install --hook-type commit-msg`.

### Changed

- Upgrade `github.com/google/go-github` from v85 to v86.

## [7.40.7] - 2026-05-06

### Changed

- Dependency updates

## [7.40.6] - 2026-05-04

### Changed

- Update `dispatch-update-chart-events` workflows to now send closed PRs events to a central repository.

## [7.40.5] - 2026-04-24

### Fixed

- Fix whitespace in pre-commit workflow for Helm charts

## [7.40.4] - 2026-04-24

### Fixed

- Code-wise nothing changed compared to v7.40.3. However, the build pipeline changed, so this release
  hopefully includes commit SHAs for each template modification.

## [7.40.3] - 2026-04-24

### Changed

- Bumped architect-orb to v7.0.0 and applied `clone_depth: 0` to the go-build job, to fix the problem that all
  generated files' headers pointed to the HEAD commit for the last change.

## [7.40.2] - 2026-04-24

### Changed

- `gen precommit`: Removed badges from helm-docs template, to avoid workflow failures due to version changes
  outside the PR.

## [7.40.1] - 2026-04-24

### Changed

- `gen precommit`: Also trigger for branch `master` in addition to `main`

### Fixed

- `gen precommit`: Fix line ending, pin action version to full semver

## [7.40.0] - 2026-04-23

### Added

- Extended command `gen precommit` to generate a Github workflow to run pre-commit in CI

## [7.39.0] - 2026-04-15

### Added

- Add new `sync-from-upstream` and `dispatch-update-chart-events` workflows to apps that have the
  `update-chart` workflow enabled.

## [7.38.0] - 2026-04-14

### Changed

- Change workflow "Fix go vulnerabilities": Set default branch to `main` for manual workflow execution
- Prevent major version bumps of components (e.g. cluster provider charts) when using `--bump-all` in minor
  releases.

## [7.37.2] - 2026-04-09

### Fixed

- Pass GitHub token to the update checker to avoid unauthenticated API rate limits.

## [7.37.1] - 2026-04-08

### Added

- When setting up helm chart tools, bootstrap `helm/<chart>/README.md.gotmpl` if it doesn't exist

## [7.37.0] - 2026-04-08

### Added

- `devctl gen precommit`: Generates opinionated `.pre-commit-config.yaml` file based on projects language,
  flavours and name

### Changed

- Exclude `giantswarm/github-workflows` from Renovate digest pinning to keep `@main` references in workflow
  templates.

## [7.36.0] - 2026-03-06

### Added

- Support Go main module in other source file `cmd/main.go` for new, kubebuilder-based projects
- Add optional `analyze-github-actions` workflow for GitHub Action security scanning.
- Add exception mechanism for GitHub Action security scanning.

### Changed

- Change permissions for Update Chart workflow from "contents: read" to "contents: write" to allow PR
  creation.

## [7.35.0] - 2026-03-03

### Added

- Releases: Add `kube-vip`.

## [7.34.1] - 2026-03-02

### Added

- Added an extra step to Chainsaw testing workflow to install extra resources.
- Add `--grouping` flag to `pr approve-merge-renovate` with values `dependency` (default) and `repo`, for
  controlling how PRs are grouped in interactive mode. When set to `repo`, PRs are grouped by repository and
  the table shows the dependency being updated.

### Changed

- Rename `PRGroup.DependencyName` to `PRGroup.Name` for clarity since the field can hold either a dependency
  or repository name.
- Make PR group sorting deterministic by adding alphabetical tiebreak when groups have equal PR counts.

## [7.34.0] - 2026-02-24

### Added

- Include `cluster` chart changelog in release notes.

## [7.33.1] - 2026-02-18

### Fixed

- Add EKS to provider title and doc maps so release notes and announcements show the correct provider name.

## [7.33.0] - 2026-02-06

### Removed

- Remove the `--enable-floating-major-tags` and templating of the `ensure_major_version_tags` workflow.

## [7.32.0] - 2026-02-06

### Changed

- Migrate `create_release` workflow to call reusable workflow from `giantswarm/github-workflows`.

### Removed

- Remove code signing support from `create_release` workflow (no longer functional).

## [7.31.0] - 2026-02-04

### Changed

- Migrate `ensure_major_version_tags` workflow to call reusable workflow from `giantswarm/github-workflows`.
- Migrate `cluster_app_documentation_validation` workflow to call reusable workflow from
  `giantswarm/github-workflows`.
- Migrate `cluster_app_schema_validation` workflow to call reusable workflow from
  `giantswarm/github-workflows`.
- Migrate `cluster_app_values_validation_using_schema` workflow to call reusable workflow from
  `giantswarm/github-workflows`.
- Migrate `helm_render_diff` workflow to call reusable workflow from `giantswarm/github-workflows`.
- Migrate `update_chart` workflow to call reusable workflow from `giantswarm/github-workflows`.

## [7.30.6] - 2026-02-03

### Added

- Add aliases `upgrade` to `devctl version update` command, so it can be called as `devctl version upgrade`.

### Fixed

- Fix string quoting in `fix_vulnerabilities` workflow template: use single quotes for string literals in
  GitHub Actions expressions.

## [7.30.5] - 2026-02-02

### Fixed

- Set default log level for fix_vulnerabilities workflow template to `info`.

## [7.30.4] - 2026-02-02

### Fixed

- Fixed `create_release` workflow template: pass PAT via `token` input to `ncipollo/release-action` instead of
  env var.

## [7.30.3] - 2026-02-02

### Fixed

- Fixed `create_release` workflow template: added `persist-credentials: false` to checkout steps that push
  using PAT credentials, preventing git from using cached GITHUB_TOKEN credentials.
- Fixed permissions for OSSF Scorecard workflow.

## [7.30.2] - 2026-01-30

### Fixed

- Fixed permissions for workflow templates calling reusable workflows. The calling workflow must grant
  permissions that the reusable workflow's jobs need.

## [7.30.1] - 2026-01-30

### Changed

- Restricted GITHUB_TOKEN permission in all generated workflows
- Change name of generated chainsaw example to `chainsaw-test.yaml`.

## [7.30.0] - 2026-01-23

### Added

- Releases: Add `node-problem-detector`.

## [7.29.0] - 2026-01-22

### Changed

- fix-vulnerabilities GH action supports log_level variable
- Releases: Disable version auto-detection for `cloud-provider-aws`.

## [7.28.1] - 2026-01-20

### Changed

- Rename the `_steps-templates` in `Test Kyverno Policies with Chainsaw` example test.

## [7.28.0] - 2026-01-19

### Added

- Add new `Test Kyverno Policies with Chainsaw` workflow and makefiles to gen commands.

## [7.27.0] - 2026-01-16

### Changed

- Replace `github.com/giantswarm/release-operator/v4/api/v1alpha1` with
  `github.com/giantswarm/releases/sdk/api/v1alpha1` for Release CRD types.

### Fixed

- Fix `getLatestGithubRelease` to return the semantically highest version instead of the most recently created
  GitHub release. This prevents backport releases (e.g., `v5.4.0`) from being incorrectly selected over newer
  versions (e.g., `v6.4.x`) when auto-bumping components.

## [7.26.1] - 2026-01-09

### Fixed

- Fix the problem in `pr approve-merge-renovate` that a query like `@actions/core` would fail due to the
  leading `@` sign.

## [7.26.0] - 2026-01-08

### Changed

- `pr approve-merge-renovate` now supports interactive mode. When called without arguments, it groups Renovate
  PRs by dependency and presents an interactive selector for choosing which group to process.

## [7.25.0] - 2026-01-06

### Added

- Added instructions to the Go-specific LLM rules to perform formatting checks.

### Changed

- Changed `devctl pr approve-align-files` to
  - also include PRs where the filter `status:success` would not match, e. g. no checks, pending checks.
  - process PRs in parallel

## [7.24.1] - 2025-12-19

### Added

- Releases: Add `vsphere-csi-driver`.

## [7.24.0] - 2025-12-18

### Added

- Add command `pr approve-merge-renovate` to approve and merge Renovate PRs

### Fixed

- Fixed problem in `pr approve-align-files` command where the repository owner could not be detected.

## [7.23.2] - 2025-12-11

### Fixed

- LLM rules for Go: fix globs syntax

## [7.23.1] - 2025-12-04

### Changed

- Go: Update dependencies.

## [7.23.0] - 2025-12-03

### Fixed

- Fix `bumpall` logic to correctly respect version constraints from `requests.yaml` when finding the latest
  version of a component or app.

## [7.22.0] - 2025-11-20

### Added

- Add support for `Breaking Changes` category in release notes generation.

## [7.21.0] - 2025-11-14

### Added

- Add `priority-classes` as a new app in v34 release.

### Fixed

- Fix lookup for new releases for certain apps.

## [7.20.4] - 2025-11-07

### Changed

- Replace "Add issue to general customer board" workflow with reusable workflow call
- Fix and tweak Cursor rules so that they actually get applied

## [7.20.3] - 2025-10-31

### Changed

- Releases: Update Flatcar tag URL.

## [7.20.2] - 2025-10-21

### Added

- Add `capa-karpenter-taint-remover` application support.

## [7.20.1] - 2025-10-17

### Added

- Add extra credentials to `fix-vulnerabilities` workflow. Needed for OSS Index access.

## [7.20.0] - 2025-10-15

### Added

- Release: Add validation to prevent type conflicts when adding apps/components. When trying to add a
  component that exists as an app (or vice versa), an error message suggesting the correct flag to use is now
  displayed.

## [7.19.0] - 2025-10-07

### Added

- Release: Add `--preserve-readme` flag to preserve existing README.md when using `--overwrite`.
- Release: Add `--regenerate-readme` flag to regenerate README.md with full changelogs when using
  `--update-existing` with specific app/component updates.

### Fixed

- Release: Fix README.md generation with `--regenerate-readme` to include all apps, not just the requested
  ones.

## [7.18.1] - 2025-10-07

### Changed

- Releases: always use UTC to format `releaseTimestamp` field for consistency

## [7.18.0] - 2025-10-02

### Added

- Releases: Add Karpenter and Karpenter Crossplane Resources.

## [7.17.0] - 2025-10-01

### Changed

- Releases: Remove Karpenter Bundle from CAPI v33.0.0 and higher.

## [7.16.0] - 2025-10-01

### Removed

- Removed AutoDetect feature for Azure components.

## [7.15.1] - 2025-10-01

### Fixed

- Fixed component auto-detection for Azure components that were renamed with `-app` suffix:
- Fixed component auto-detection for `os-tooling` to correctly map to `capi-image-builder` repository

## [7.15.0] - 2025-09-30

### Changed

- Releases: Remove CAPI Node Labeler from CAPI v33.0.0 and higher.

## [7.14.0] - 2025-09-18

### Added

- Add `--update-existing` flag to `devctl release create` to update an existing release in the current branch
  instead of creating from a base release.
- When using `--update-existing` with specific `--component` or `--app` flags, the command now preserves all
  previous modifications and only updates the specified components/apps. This enables incremental updates
  across multiple invocations.
- Fixed release diff generation to show meaningful diffs when using `--update-existing` by comparing against
  the previous release version.
- Fixed bug where `dependsOn` fields were being cleared when updating apps.

### Changed

- Replaced `--from-branch` flag with `--update-existing` for better clarity.

## [7.13.0] - 2025-09-18

### Changed

- Make `--base` flag optional for certain use cases.

## [7.12.1] - 2025-09-17

### Fixed

- Prevented duplicate output when using the `--output markdown` flag by correcting the table rendering logic.

## [7.12.0] - 2025-09-17

### Changed

- `devctl release create --bumpall`:
  - The release type (`major`, `minor`, `patch`) is now inferred from the `--base` and `--name` versions
    instead of requiring a separate flag.
  - The release type is used to enforce version bumping rules for `kubernetes`, `flatcar`, and other
    components.
  - Includes version constraints defined in provider-specific `requests.yaml` files.

### Fixed

- Fix a table rendering issue in the `devctl release create --bumpall` output.

## [7.11.1] - 2025-09-11

### Changed

- Releases: Allow revisions in versions.

## [7.11.0] - 2025-09-10

### Changed

- Environment: Unify GitHub token retrieval.

## [7.10.5] - 2025-09-10

### Changed

- Releases: Rework release notes templating.

## [7.10.4] - 2025-09-10

### Changed

- Releases: Disable auto-detect for CAPVCD & CAPV, fix dependency concatenation.

## [7.10.3] - 2025-09-10

### Changed

- Releases: Fix split character for dependencies.

## [7.10.2] - 2025-09-09

### Fixed

- Correctly pin Kubernetes version to the release's major version during `devctl release create --bumpall` to
  prevent accidentally upgrading to a new minor version.

## [7.10.1] - 2025-09-09

### Fixed

- Prevent auto-detected components from being shown when using `--requested-only`.
- Prevent empty tables from being printed when using filtering flags like `--changes-only` or
  `--requested-only`.

## [7.10.0] - 2025-09-09

### Added

- Add `--requested-only` flag to `devctl release create` command to only show components and apps requested by
  the user.

## [7.9.0] - 2025-09-09

### Added

- Add `--changes-only` flag to `devctl release create` command to only show components and apps that were
  changed.

## [7.8.0] - 2025-09-09

### Added

- Add `--output markdown` flag to `devctl release create` command to print the output in Markdown format.

## [7.7.2] - 2025-09-05

### Changed

- Remove trailing whitespace from LLM rules, which caused some linter complaints in CI.

## [7.7.1] - 2025-09-03

### Changed

- Releases: Fix documentation link in announcement.

## [7.7.0] - 2025-09-03

### Changed

- `devctl release create`: Improve auto-detection of component versions to be more generic and maintainable.

### Fixed

- `devctl release create`: Fix auto-detection for Azure provider components.
- `devctl release create --bumpall`: Fix an issue where dependencies were dropped for automatically bumped
  apps.
- `devctl release create`: Stop parsing component changelogs when the specified `endVersion` is not found,
  preventing huge outputs.

## [7.6.0] - 2025-08-27

### Changed

- `devctl release create`:
  - Drop `karpenter-nodepools` from AWS releases v32.0.0 and higher.
  - Add generic support for dropping apps from releases based on release version.
  - Add support for defining app dependencies via the `--app` flag.
  - Improve help text with more detailed and accurate examples.
- `devctl release create --bumpall`:
  - Improve UX by coloring bumped versions green and removed apps red in the summary table.
  - Display app dependencies in the summary table.

## [7.5.3] - 2025-08-26

### Fixed

- Ignore values check with new values file against older helm releases.

## [7.5.2] - 2025-08-26

### Changed

- Workflows: Skip release creation if already exists.

## [7.5.1] - 2025-07-31

### Changed

- Releases: Fix candidate name logic.

## [7.5.0] - 2025-07-28

### Added

- Add `devctl gen llm` to generate standard Giant Swarm LLM rules in a repository.

## [7.4.2] - 2025-06-27

### Fixed

- Enabled installing via the `go install github.com/giantswarm/devctl/v7@latest` command by removing replace
  directives in `go.mod`.

### Changed

- Use `main` when referencing reusable GitHub workflows.

## [7.4.1] - 2025-06-26

### Fixed

- Fix changelog validation workflow for release PRs.

## [7.4.0] - 2025-06-26

### Added

- Add changelog validation workflow for release PRs.

## [7.3.0] - 2025-06-24

## [7.2.7] - 2025-06-11

### Changed

- Align release creation with our current approach.
- The following GitHub workflows are now calling external, reusable workflows in
  `giantswarm/github-workflows`:
  - "Scorecard supply-chain security"
  - "Validate chart values and schema"
- Fix line ending in workflow template "Fix Go vulnerabilities"

## [7.2.6] - 2025-06-03

### Changed

- Update "Publish TechDocs" workflow reference
- The following GitHub workflows are now calling external, reusable workflows in
  `giantswarm/github-workflows`:
  - "Fix vulnerabilities"

## [7.2.5] - 2025-05-30

### Changed

- The following GitHub workflows are now calling external, reusable workflows in
  `giantswarm/github-workflows`:
  - "Create Release"
  - "Create Release PR"
  - "gitleaks"

## [7.2.4] - 2025-05-16

### Added

- `gen workflows`: Add option to generate "Publish TechDocs" workflow via the `--publish-techdocs` flag.

## [7.2.3] - 2025-05-15

### Added

- Add `devctl pr approve-align-files` - help engineers to approve "Align files" PRs

## [7.2.2] - 2025-05-14

### Changed

- Update `nancy-fixer` in `fix_vulnerabilities` workflow

## [7.2.1] - 2025-05-14

### Changed

- Pin dependency `giantswarm/gitleaks-action` version in gitleaks workflow

### Fixed

- Fix workflow syntax of "Validate Cluster Values using JSON Schema" workflow

## [7.2.0] - 2025-04-23

### Added

- Added `deploy` command to deploy applications to GitOps repositories
- Added experimental `bootstrap` command

### Changed

- Pin ubuntu version in CI to use 24.04.
- Validate cluster values workflow: ignore default branch

## [7.1.4] - 2025-03-06

- Updates in various actions used in generated workflows

## [7.1.3] - 2025-02-07

- Dependency updates

## [7.1.2] - 2025-01-31

### Fixed

- Do not duplicate release info in `releases.json` when re-creating a release

### Changed

- Dependency updates

## [7.1.1] - 2025-01-23

### Fixed

- Skip changelog if apps or components haven't changed.

## [7.1.0] - 2025-01-15

### Added

- Add all app and component version changes since last release to the release notes.

## [7.0.1] - 2025-01-10

- Updating some GitHub action versions

## [7.0.0] - 2024-12-13

**Note** Creating vintage releases is not supported anymore, please use an older release of `devctl`.

### Added

- Create releases for CAPI providers.

## [6.32.0] - 2024-12-12

- Dependency updates

## [6.31.0] - 2024-10-30

### Added

- Add new `fleet` flavour.
- Add workflow to validate cluster-app values against the schema to the `fleet` projects.

## [6.30.0] - 2024-10-14

- Dependency updates in templates

## [6.29.0] - 2024-10-08

### Added

- Add Backstage specific job to "Create release PR" workflow template.

## [6.28.0] - 2024-09-20

### Added

- Add `cluster-*` charts as choosable values for `devctl release create --component`

## [6.27.2] - 2024-08-22

### Changed

- Update Go and architect in "Create release PR" workflow template

## [6.27.1] - 2024-08-22

### Fixed

- Bump `github.com/marwan-at-work/mod/cmd/mod` to `v0.7.1` in the "Create release PR" workflow template, to
  fix compatibility with Go 1.23 an re-enable majore version relases.

## [6.27.0] - 2024-08-20

### Fixed

- Include replace directives in apptest generated go.mo

### Changed

- Add `-trimpath` flag to `go build` in generated Makefile for Go.

## [6.26.4] - 2024-06-13

- Updates in referenced actions and dependencies

## [6.26.3] - 2024-05-21

### Fixed

- Handle renamed function in Apptest template

## [6.26.2] - 2024-04-25

### Fixed

- Only fetch `main` branch in GitHub actions workflow for devctl releases

## [6.26.1] - 2024-04-25

### Fixed

- Add logic to fetch the whole git history when generating the "Create Release" GitHub actions workflow file
  for devctl.
- Fetch whole git history for releases to fix GitHub Urls in templated file headers.

## [6.26.0] - 2024-04-24

### Fixed

- Compare Helm Rendering (only used for cluster charts): create diff comment from file to avoid size limit

## [6.25.1] - 2024-04-22

### Fixed

- Set the CI webhook secret
- Made `generate-go` Make task show up in `make help` and added a note to the readme about the template
  generation.

## [6.25.0] - 2024-04-18

### Added

- Add `--bumpall` flag to `release create` command to automatically bump all apps and components to the latest
  version.

## [6.24.0] - 2024-04-10

### Removed

- devctl version is no longer added to generated file header

### Changed

- Generated file headers now include a GitHub link

## [6.23.3] - 2024-03-26

### Changed

- Pin generated GitHub Action workflows to SHAs.
- Rename GitHub Action `jungwinter` to `winterjung`.
- (Renovate) automatically bump GitHub action versions in generated workflows.

## [6.23.2] - 2024-03-21

### Changed

- Update OSSF Scorecard GitHub Action to v2.3.1.

## [6.23.1] - 2024-03-15

### Added

- Added a default providers array to E2E apptest config

## [6.23.0] - 2024-03-15

### Changed

- Add a new permission for workflows to be able to write in the repo
- Update actions/setup-go to v5 in generated workflows

## [6.22.0] - 2024-03-11

### Added

- Added a new `ci-webhooks` command under the `repo setup` command that configures webhooks to our Tekton
  installation
- Added a new `gen apptest` command that creates the files needed by apptest-framework

## [6.21.0] - 2024-03-07

### Changed

- Compare Helm Rendering (only used for cluster charts): put matrix of diff rendering comments into single
  comment and upgrade dependencies

## [6.20.2] - 2024-02-06

### Changed

- Fix `giantswarm/install-binary-action` version.
- Update `nancy-fixer` to v0.4.3 in generated workflow.

## [6.20.1] - 2024-02-02

### Changed

- Update giantswarm/install-binary-action to v2.0.0 in generated workflows
- Update nancy-fixer to v0.4.2 in generated workflow.

## [6.20.0] - 2024-01-31

### Added

- Add a `Fix Vulnerabilities` workflow to remediate Nancy findings.

## [6.19.0] - 2024-01-31

### Added

- Include cluster-test-catalog in "Compare Helm Rendering" action, so we can more easily test dev builds of
  subcharts.

## [6.18.3] - 2024-01-31

### Changed

- Enable automatic merging of the "Bump version in project.go" PR.

## [6.18.2] - 2024-01-19

### Changed

- Update `architect` to v6.14.1 (with go version v1.21.6)
- Use alpine and signcode images from gsoci.azurecr.io

## [6.18.1] - 2024-01-10

### Fixed

- Fix script injection vulnerability in "Create Release" GitHub action template.

## [6.18.0] - 2023-12-18

### Added

- Add support for generating OpenSSF Scorecard workflows.

## [6.17.2] - 2023-11-28

### Fixed

- Fix Compare Helm Rendering action, so that, when it renders Helm with main branch code, it takes the CI test
  values from the main branch and not from the current branch.

## [6.17.1] - 2023-11-23

- Replace unmaintained GitHub action for release creation in "Create Release" workflow with
  `ncipollo/release-action`.

## [6.17.0] - 2023-11-14

- Validate documentation generated from JSON schema (for cluster apps)

## [6.16.0] - 2023-11-08

### Changed

- Update `architect` to v6.13.0 (with go version v1.21.3)

## [6.15.1] - 2023-10-31

### Fixed

- Prevent false positives in nancy's vulnerability reports by using `go list` with `-deps ./...`

## [6.15.0] - 2023-10-24

### Changed

- Changed the Go module name to `github.com/giantswarm/devctl/v6`

## [6.14.0] - 2023-10-20

### Changed

- Edited Gitleaks to use our own repo, which removed the deprecated `set-output` command.
- Enable Renovate to access the repo by default as part of `devctl repo setup`
- Switch renovate to using a JSON5 config file

## [6.13.0] - 2023-10-05

### Changed

- Replaced `hub` with `gh` in CI templates.
- Override GH Action workflows replacing `hub` with `gh`.

### Fixed

- Fixed inconsistent logging in `devctl repo setup renovate`.

## [6.12.0] - 2023-09-28

### Added

- Add `devctl repo setup renovate` to enable/disable Renovate for a repository.

## [6.11.0] - 2023-09-22

### Changed

- `devctl gen renovate`: make `--interval` optional, remove default value

## [6.10.0] - 2023-09-15

### Changed

- Update `action/checkout` to `v4` in Github Action template files.

## [6.9.0] - 2023-09-14

### Changed

- Let renovate ignore dependency `github.com/imdario/mergo`.

## [6.8.0] - 2023-09-12

### Changed

- Bump release operator dependency to v4 to add support for dependencies on release apps.
- Add some apps to the changelog apps list.
- Change `gen ami` command in order to work with aws-operator >= 14.22.0 where AMI Ids have been moved to the
  config repo.

### Fixed

- Fix AMI ID detection for china in `gen ami` command.

## [6.7.0] - 2023-08-18

### Changed

- Bumped Ubuntu in Github workflow runners to v22.04

## [6.6.0] - 2023-08-14

### Changed

- Exclude `.github/workflows/pre_commit_*.yaml` from renovate dependency updates, as this file is managed
  centrally.

## [6.5.0] - 2023-07-27

### Changed

- Updated the `update-chart` PullRequest template to include additional hints.

## [6.4.0] - 2023-06-26

### Changed

- Helm schema validation GitHub Action now skips when the `values.schema.json` file is not present for the
  Helm chart
  - Repositories can contain multiple Helm chart, only folders that does not have the file will be skipped and
    the check will be considered successful for those folders

### Removed

- Changelog: Remove `nginx-ingress-controller`. ([#595](https://github.com/giantswarm/devctl/pull/595))

## [6.3.1] - 2023-06-02

### Fixed

- Fix "Compare Helm Rendering" workflow to use correct CI values path when rendering the default branch
  version.

## [6.3.0] - 2023-06-01

### Added

- For flavor `cluster-app`, the make target `generate-docs` is added, to generate Markdown documentation on
  values.

## [6.2.0] - 2023-06-01

### Removed

- Remove reviewer from renovate file as we rely on Github `CODEOWNERS` file instead.

## [6.1.1] - 2023-05-11

### Fixed

- Fix Github action that renders Helm templates on "cluster-app" repositories, by using variables instead of
  hardcoding repositories names and branches.

## [6.1.0] - 2023-05-09

### Added

- Github action to render Helm templates on "cluster-app" flavour repositories.

### Changed

- Makefile help target: accept `/` and `%` (automatic target) in target name

## [6.0.0] - 2023-05-08

### Fixed

- CircleCI badge in devctl's own README

### Removed

- Remove `gen kubeconfig` command

## [5.24.0] - 2023-05-02

### Changed

- Update schemalint to v2

## [5.23.0] - 2023-04-18

### Added

- Add `cilium-prerequisites` component.
- Add `--disable-branch-protetion` flag to `devctl repo setup`, to allow disabling github branch protection.

### Changed

- Check values schema on push, not only for PRs

## [5.22.0] - 2023-04-13

### Changed

- Bump `github.com/marwan-at-work/mod/cmd/mod` to `v0.5.0` in create release pr template
- Update `architect` to v6.11.0

## [5.21.1] - 2023-04-06

### Changed

- Update comment in the cluster-app schema validation workflow file.
- Replaced `upload-release-assets` action-based step with CLI-based step.

### Added

- Add help text to cluster-app schema make file

## [5.21.0] - 2023-03-30

### Changed

- Change github identity to taylorbot in generated workflows
- repo setup: rename default branch to main

### Fixed

- Fix a bug where open pull-request are not correctly detected
- Incorrectly attempting to bump to `/v2` when releasing `v1.0.0`

## [5.20.1] - 2023-03-24

### Fixed

- repo setup: filter aliyun checks out of required checks for PR merge

## [5.20.0] - 2023-03-22

## [5.20.0] - 2023-03-22

### Added

- Add `--dry-run` flag to `devctl repo setup` command.

### Fixed

- Add new flavour with workflows and makefile for cluster apps.

### Changed

- repo setup: better select checks required for PR merge
- Fix unsafe pointer access in pkg/githubclient/client_repository.go

## [5.19.0] - 2023-02-21

### Changed

- Update used go version in generated workflows to v1.19.6.
- Update `architect` to v6.10.0 (with go version v1.19.6).

## [5.18.3] - 2023-02-17

### Changed

- Merge `ci/ci-values.yaml` with `values.yaml` before doing the schema validation.
- Update `Makefile` to prevent recursion when looking for deps.
- Use `GITHUB_SHA` in values validation workflow in git diff. This makes the action work with contributions
  from external repositories.

## [5.18.2] - 2023-01-31

### Changed

- Remove recursion from `Validate values.yaml schema` workflow.

## [5.18.1] - 2023-01-26

### Changed

- Added more information to the `update-chart` PullRequest template.

## [5.18.0] - 2023-01-23

### Changed

- Update giantswarm/install-binary-action to v1.1.0 in generated workflows

## [5.17.0] - 2023-01-16

### Fixed

- Fix etcd changelog parsing settings.
- Fix regexp to allow matching releases having suffixes.

## Added

- Add a bunch of new default apps.

## [5.16.0] - 2022-12-20

### Changed

- Change Makefile target `update-deps` to only check chart dependencies with a local `Chart.yaml` in generated
  app Makefile template

## [5.15.0] - 2022-12-15

### Added

- Add new flavour to generate a customer workflow

### Changed

- Catch release/latest "Not Found" in workflow `create_release_pr`
- Update vendir to [v0.32.2](https://github.com/vmware-tanzu/carvel-vendir/releases/tag/v0.32.2) in
  update_chart workflow

## [5.14.0] - 2022-12-02

### Changed

- Switched values schema validator to [yajsv](https://github.com/neilpa/yajsv) v1.4.1.

## [5.13.1] - 2022-12-01

### Fixed

- Fix syntax in `check_values_schema.yaml.template`

## [5.13.0] - 2022-12-01

### Changed

- Simplify the schema check action for helm + do the actual schema validation (#464)

## [5.12.0] - 2022-11-09

### Added

- Add update-chart target in Apps makefile.
- Add helm-docs target in Apps makefile.
- Add update_chart workflow for app flavored repos.

## [5.11.1] - 2022-10-24

### Changed

- Replaced deprecated set-output with env var alternative

## [5.11.0] - 2022-10-05

### Changed

- Update used go version in generated workflows to v1.19.1.
- Update `architect` to v6.7.0 (with go version v1.19.1).
- Update `setup-go` action to v3.3.0.

## [5.10.0] - 2022-09-23

### Fixed

- Bump go module also when releasing a version with a suffix like `-alpha1`.
- Add `renovate` label to RenovateBot PRs.

## [5.9.0] - 2022-07-14

### Changed

- Completely rework check_values_schema action to cut down on noise

## [5.8.0] - 2022-07-12

### Added

- Add `nancy` command to Go makefile for a convenient method to run the Nancy checks the same way as they are
  done on the CI

### Changed

- Align git author identities throughout PR automation workflows

## [5.7.0] - 2022-06-23

### Added

- Updating of version field in Chart.yaml of helm charts

## [5.6.1] - 2022-06-20

### Changed

- Modify windows build script to make code signing optional and skip if it's not configured

## [5.6.0] - 2022-06-20

### Changed

- Update GitHub workflow to not fail all matrix build on one failure

## [5.5.0] - 2022-06-16

### Changed

- Split long description into short and long description fields.

### Fixed

- Fix `redefine 'l' shorthand in "makefile" flagset` error.

## [5.4.0] - 2022-06-13

### Added

- Add `devctl repo setup` command. Setup github repository settings and permissions.

## [5.3.1] - 2022-06-08

### Changed

- Update github.com/marwan-at-work/mod/cmd/mod to v0.4.2 to include fix:
  https://github.com/marwan-at-work/mod/pull/14

## [5.3.0] - 2022-05-10

### Changed

- Update used Go version in generated workflows to 1.18.1.
- Update `architect` to v6.4.0 (with go version 1.18.1).

## [5.2.1] - 2022-04-26

### Added

- Added file system permissions field to file generation `Input` struct. If not set, the default value
  remains: `0644`.
- Added executable flags for generated `windows-code-signing.sh` script.

### Fixed

- Fixed quotation in generated `windows-code-signing.sh` to prevent globbing and word splitting issues.

## [5.2.0] - 2022-04-20

### Added

- Build signed Windows binaries for CLIs

## [5.1.2] - 2022-04-14

### Fixed

- Invalid quoting caused schema checking to not use branch names from environment.

## [5.1.1] - 2022-04-12

### Fixed

- Make values schema checking resilient against slashes in branch names.

## [5.1.0] - 2022-04-08

### Changed

- Change release automation so that it automatically bumps `go.mod` module version when releasing a new major
  release.

## [5.0.0] - 2022-04-04

### Changed

- Remove `apiextensions` dependency.
- Upgrade `github.com/giantswarm/k8sclient` to `v7.0.1`.
- Upgrade `github.com/giantswarm/kubeconfig` to `v4.1.0`.
- Upgrade `k8s.io/apimachinery` to `v0.20.12`.

## [4.24.1] - 2022-04-01

### Fixed

- Make codesign parameters in `gen makefile --flavour cli --language go` for windows generic

## [4.24.0] - 2022-04-01

### Added

- Add steps to build signed windows binary in `gen makefile --flavour cli --language go`

## [4.23.0] - 2022-03-31

### Added

- Creation of GitHub workflow file to validate values.schema.json if it exists for
  `gen workflows --flavour app`.

## [4.22.0] - 2022-03-30

### Added

- Add more apps for release notes.

### Fixed

- Fix fetching flatcar release notes.

## [4.21.0] - 2022-03-04

### Changed

- Update used go version in generated workflows to 1.17.8.
- Update `architect` to v6.3.0 (with go version 1.17.8).

## [4.20.1] - 2022-03-02

### Fixed

- Forgot to update one `actions/checkout` in create_release_pr workflow template.

## [4.20.0] - 2022-03-02

### Changed

- Update `actions/checkout` action to v3 in generated workflows.

## [4.19.0] - 2022-02-18

### Fixed

- Fix repo_name in generated release PR workflow.

## [4.18.0] - 2022-02-16

### Added

- Let renovate add the `dependencies` label to every PR it creates.

## [4.17.0] - 2022-02-11

### Changed

- Update `setup-go` action to v2.2.0 in generated workflows.
- Update used go version in generated workflows to 1.17.7.
- Update `architect` to v6.2.0 (with go version 1.17.7).

## [4.16.1] - 2022-02-09

### Fixed

- Fixed exclusion of CAPI dependencies.

## [4.16.0] - 2022-02-07

### Changed

- Update `setup-go` action to v2.1.5.
- Update `architect` to v6.1.0.

## [4.15.0] - 2022-02-02

### Added

- Add `k8sapi` flavour to `gen` commands.

### Changed

- Upgrade `create_release_pr` to accept branches without base ref.
- Renovate exclude cluster-api dependencies.
- Renovate only suggest giantswarm/apiextensions >= 4.0.0.

## [4.14.0] - 2022-01-26

### Changed

- Upgrade used `architect` version in generated `create_release` and `create_release_pr` workflows to `5.3.0`.
- Upgrade used go version in generated `create_release` workflow to `1.17.6`.
- Add handling of Semver verbs to generated `create_release_pr` workflow.

## [4.13.1] - 2022-01-24

### Fixed

- Set repo name correctly when calling from other workflow

## [4.13.0] - 2022-01-17

### Added

- Added support for calling `create_release_pr` from another workflow.

## [4.12.0] - 2021-12-21

### Added

- Add changelog entries to release body.

### Fixed

- Fix `Get version` job failing with some commit messages.

## [4.11.0] - 2021-11-26

### Added

- Include release notes for app `aws-ebs-csi-driver`.

### Changed

- Upgrade `fsaintjacques/semver-tool` to `3.2.0` to fix problem with releases with high minor version.

## [4.10.0] - 2021-09-10

- fix description for name flag at archive release command
- Upgrade used `architect` version in generated `create_release` and `create_release_pr` workflows to `5.2.0`.
- Upgrade used go version in generated `create_release` and `create_release_pr` workflows to `1.17.1`.

## [4.9.2] - 2021-08-18

## Changed

- fix: added new language type 'python'

## [4.9.1] - 2021-08-17

## Changed

Renovate config

- ignore updates of stuff generated by our automation (github actions, architect version)
- add global renovate dashboard
- add python project default config

## [4.9.0] - 2021-08-12

## Changed

- Update k8s version limit in renovate.
- `--reviewer` option from `devctl gen renovate` is not required anymore.

## [4.8.0] - 2021-08-11

## Added

- Add `devctl gen renovate` command.

- Dependencies: use github.com/gorilla/websocket version v1.4.2

## [4.7.0] - 2021-07-08

## Added

- Add additional file detection for `pip` dependabot generation.

## [4.6.1] - 2021-06-21

## Changed

- Disable `gitleaks` on push trigger.

## [4.6.0] - 2021-06-21

## Changed

- Fix release templating.

## [4.5.2] - 2021-06-21

## Changed

- Update `gitleaks action` to version `1.6.0`.

## [4.5.1] - 2021-05-04

### Fixed

- Fix caching for self-update mechanism.

## [4.5.0] - 2021-05-04

### Added

- Add `--enable-floating-major-tags` to `devctl gen workflows`.
- Add `devctl version check`.
- Add `devctl version update`.
- Check for latest version before running commands.
- Make generated Makefile help target on par with kubebuilder.

## [4.4.0] - 2021-03-19

### Added

- Add azure-scheduled-events to known apps for `gen release`.
- Add darwin-arm64 and linux-arm64 build targets to generated Makefile and GitHub workflows.
- Upgrade used `architect` version in generated `create_release` and `create_release_pr` workflows to `3.4.0`.
- Upgrade used go version in generated `create_release` and `create_release_pr` workflows to `1.16.2`.

## [4.3.0] - 2021-02-15

### Added

- Allow specifying multiple flavours for `gen makefile` and `gen workflows`.

### Fixed

- Fix the binary name in docker-build target in makefile generated for Go.

## [4.2.1] - 2021-02-01

### Fixed

- Fix broken formatting in Create Release workflow.

## [4.2.0] - 2021-02-01

### Fixed

- Add `main` branch as release target for Create Release workflow.

## [4.1.0] - 2021-01-29

### Fixed

- Add `main` branch as release target for Create Release PR workflow.

## [4.0.2] - 2021-01-14

### Fixed

- Compile binaries statically only on Linux to avoid linking issues on other platforms in generated Makefiles
  for Go.
- Fix generated Makefile for Go language for cases where there are no Go source files in the root module
  directory. In particular `make imports` is fixed.
- Fix open PR check in generated "Create release PR" workflow.

## [4.0.1] - 2020-12-14

## Changed

- Update `gitleaks action` to version `1.2.0` using `gitleaks` version `7.2.0`.

## [4.0.0] - 2020-12-08

## Removed

- Remove `go mod tidy` workflow.

## Added

- Add "npm" and "pip" ecosystems to `gen dependabot`.
- Add Dockerfile.
- Generate main Makefile including `*.mk` files to allow custom Makefiles.
- Pretty print errors.
- Print devctl version in generated files headers.

## Changed

- Rename ecosystem "go" to "gomod" in `gen dependabot`.
- Generate language specific Makefiles in `gen makefile`.
- Add required `--language` flag to `gen makefile`.

## Fixed

- Fix Azure tag URL in release changelog generation.
- Do not try to create a previous release branch when tagging the first release.

## Removed

- Remove `repo list` command replaced with Go modules + dependabot.
- Remove `gen crud` command as CRUD handler is obsolete.

## [3.1.0] - 2020-11-05

### Added

- Add `gitleaks` workflow to `gen workflows`.

### Fixed

- Fix changelog collection for non-master branches in `release create` command.

## [3.0.0] - 2020-10-29

### Added

- Add `generic` flavour to `gen` commands.

### Removed

- Remove `operator` flavour from `gen` commands. `app` flavour should be used instead.
- Remove `library` flavour from `gen` commands. `generic` flavour should be used instead.

## [2.0.4] - 2020-10-21

### Fixed

- Fix generated workflows warnings. E.g. https://github.com/kopiczko/test-gh-workflows/actions/runs/319535662
- Fix regression in "Create release branch" job in generated "Create Release" workflow.

## [2.0.3] - 2020-10-20

### Fixed

- Replace leftover install-tools-action with install-binary-action in generated workflow.

### Security

- Update actions/setup-go from v1 to v2.1.3 in generated workflows.

## [2.0.2] - 2020-10-20

### Fixed

- Replace install-tools-action with install-binary-action to break circular dependency between devctl and
  install-tools-action.

## [2.0.1] - 2020-10-16

### Fixed

- Skip generated "Create Release PR" workflow execution when release PR already exists.

## [2.0.0] - 2020-10-14

### Changed

- Include k8s dependency for 1.18 in generated dependabot configuration.

### Security

- Update actions/upload-artifact to v2.2.0.
- Update actions/cache to v2.1.1.

### Fixed

- Update architect to v3.0.0 to fix the issue with updating Go module version. E.g.:
  https://github.com/giantswarm/operatorkit/commit/db6fafc711528b5d7474d2717cf7f4bb850f8812#diff-37aff102a57d3d7b797f152915a6dc16R1
- Update architect to v3.0.2 to allow release names to have suffixes.

## [1.0.0] - 2020-09-23

### Added

- First release.

[Unreleased]: https://github.com/giantswarm/devctl/compare/v8.23.0...HEAD
[8.23.0]: https://github.com/giantswarm/devctl/compare/v8.22.1...v8.23.0
[8.22.1]: https://github.com/giantswarm/devctl/compare/v8.22.0...v8.22.1
[8.22.0]: https://github.com/giantswarm/devctl/compare/v8.21.1...v8.22.0
[8.21.1]: https://github.com/giantswarm/devctl/compare/v8.21.0...v8.21.1
[8.21.0]: https://github.com/giantswarm/devctl/compare/v8.20.5...v8.21.0
[8.20.5]: https://github.com/giantswarm/devctl/compare/v8.20.4...v8.20.5
[8.20.4]: https://github.com/giantswarm/devctl/compare/v8.20.3...v8.20.4
[8.20.3]: https://github.com/giantswarm/devctl/compare/v8.20.2...v8.20.3
[8.20.2]: https://github.com/giantswarm/devctl/compare/v8.20.1...v8.20.2
[8.20.1]: https://github.com/giantswarm/devctl/compare/v8.20.0...v8.20.1
[8.20.0]: https://github.com/giantswarm/devctl/compare/v8.19.0...v8.20.0
[8.19.0]: https://github.com/giantswarm/devctl/compare/v8.18.0...v8.19.0
[8.18.0]: https://github.com/giantswarm/devctl/compare/v8.17.1...v8.18.0
[8.17.1]: https://github.com/giantswarm/devctl/compare/v8.17.0...v8.17.1
[8.17.0]: https://github.com/giantswarm/devctl/compare/v8.16.0...v8.17.0
[8.16.0]: https://github.com/giantswarm/devctl/compare/v8.15.2...v8.16.0
[8.15.2]: https://github.com/giantswarm/devctl/compare/v8.15.1...v8.15.2
[8.15.1]: https://github.com/giantswarm/devctl/compare/v8.15.0...v8.15.1
[8.15.0]: https://github.com/giantswarm/devctl/compare/v8.14.1...v8.15.0
[8.14.1]: https://github.com/giantswarm/devctl/compare/v8.14.0...v8.14.1
[8.14.0]: https://github.com/giantswarm/devctl/compare/v8.13.0...v8.14.0
[8.13.0]: https://github.com/giantswarm/devctl/compare/v8.12.0...v8.13.0
[8.12.0]: https://github.com/giantswarm/devctl/compare/v8.11.1...v8.12.0
[8.11.1]: https://github.com/giantswarm/devctl/compare/v8.11.0...v8.11.1
[8.11.0]: https://github.com/giantswarm/devctl/compare/v8.10.0...v8.11.0
[8.10.0]: https://github.com/giantswarm/devctl/compare/v8.9.0...v8.10.0
[8.9.0]: https://github.com/giantswarm/devctl/compare/v8.8.0...v8.9.0
[8.8.0]: https://github.com/giantswarm/devctl/compare/v8.7.0...v8.8.0
[8.7.0]: https://github.com/giantswarm/devctl/compare/v8.6.0...v8.7.0
[8.6.0]: https://github.com/giantswarm/devctl/compare/v8.5.0...v8.6.0
[8.5.0]: https://github.com/giantswarm/devctl/compare/v8.4.0...v8.5.0
[8.4.0]: https://github.com/giantswarm/devctl/compare/v8.3.5...v8.4.0
[8.3.5]: https://github.com/giantswarm/devctl/compare/v8.3.4...v8.3.5
[8.3.4]: https://github.com/giantswarm/devctl/compare/v8.3.4...v8.3.4
[8.3.4]: https://github.com/giantswarm/devctl/compare/v8.3.4...v8.3.4
[8.3.4]: https://github.com/giantswarm/devctl/compare/v8.3.3...v8.3.4
[8.3.3]: https://github.com/giantswarm/devctl/compare/v8.3.2...v8.3.3
[8.3.2]: https://github.com/giantswarm/devctl/compare/v8.3.1...v8.3.2
[8.3.1]: https://github.com/giantswarm/devctl/compare/v8.3.0...v8.3.1
[8.3.0]: https://github.com/giantswarm/devctl/compare/v8.2.0...v8.3.0
[8.2.0]: https://github.com/giantswarm/devctl/compare/v8.1.0...v8.2.0
[8.1.0]: https://github.com/giantswarm/devctl/compare/v8.0.0...v8.1.0
[8.0.0]: https://github.com/giantswarm/devctl/compare/v7.48.0...v8.0.0
[7.48.0]: https://github.com/giantswarm/devctl/compare/v7.47.0...v7.48.0
[7.47.0]: https://github.com/giantswarm/devctl/compare/v7.46.0...v7.47.0
[7.46.0]: https://github.com/giantswarm/devctl/compare/v7.45.0...v7.46.0
[7.45.0]: https://github.com/giantswarm/devctl/compare/v7.44.0...v7.45.0
[7.44.0]: https://github.com/giantswarm/devctl/compare/v7.43.0...v7.44.0
[7.43.0]: https://github.com/giantswarm/devctl/compare/v7.42.0...v7.43.0
[7.42.0]: https://github.com/giantswarm/devctl/compare/v7.41.1...v7.42.0
[7.41.1]: https://github.com/giantswarm/devctl/compare/v7.41.0...v7.41.1
[7.41.0]: https://github.com/giantswarm/devctl/compare/v7.40.7...v7.41.0
[7.40.7]: https://github.com/giantswarm/devctl/compare/v7.40.6...v7.40.7
[7.40.6]: https://github.com/giantswarm/devctl/compare/v7.40.5...v7.40.6
[7.40.5]: https://github.com/giantswarm/devctl/compare/v7.40.4...v7.40.5
[7.40.4]: https://github.com/giantswarm/devctl/compare/v7.40.3...v7.40.4
[7.40.3]: https://github.com/giantswarm/devctl/compare/v7.40.2...v7.40.3
[7.40.2]: https://github.com/giantswarm/devctl/compare/v7.40.1...v7.40.2
[7.40.1]: https://github.com/giantswarm/devctl/compare/v7.40.0...v7.40.1
[7.40.0]: https://github.com/giantswarm/devctl/compare/v7.39.0...v7.40.0
[7.39.0]: https://github.com/giantswarm/devctl/compare/v7.38.0...v7.39.0
[7.38.0]: https://github.com/giantswarm/devctl/compare/v7.37.2...v7.38.0
[7.37.2]: https://github.com/giantswarm/devctl/compare/v7.37.1...v7.37.2
[7.37.1]: https://github.com/giantswarm/devctl/compare/v7.37.0...v7.37.1
[7.37.0]: https://github.com/giantswarm/devctl/compare/v7.36.0...v7.37.0
[7.36.0]: https://github.com/giantswarm/devctl/compare/v7.35.0...v7.36.0
[7.35.0]: https://github.com/giantswarm/devctl/compare/v7.34.1...v7.35.0
[7.34.1]: https://github.com/giantswarm/devctl/compare/v7.34.0...v7.34.1
[7.34.0]: https://github.com/giantswarm/devctl/compare/v7.33.1...v7.34.0
[7.33.1]: https://github.com/giantswarm/devctl/compare/v7.33.0...v7.33.1
[7.33.0]: https://github.com/giantswarm/devctl/compare/v7.32.0...v7.33.0
[7.32.0]: https://github.com/giantswarm/devctl/compare/v7.31.0...v7.32.0
[7.31.0]: https://github.com/giantswarm/devctl/compare/v7.30.6...v7.31.0
[7.30.6]: https://github.com/giantswarm/devctl/compare/v7.30.5...v7.30.6
[7.30.5]: https://github.com/giantswarm/devctl/compare/v7.30.4...v7.30.5
[7.30.4]: https://github.com/giantswarm/devctl/compare/v7.30.3...v7.30.4
[7.30.3]: https://github.com/giantswarm/devctl/compare/v7.30.2...v7.30.3
[7.30.2]: https://github.com/giantswarm/devctl/compare/v7.30.1...v7.30.2
[7.30.1]: https://github.com/giantswarm/devctl/compare/v7.30.0...v7.30.1
[7.30.0]: https://github.com/giantswarm/devctl/compare/v7.29.0...v7.30.0
[7.29.0]: https://github.com/giantswarm/devctl/compare/v7.28.1...v7.29.0
[7.28.1]: https://github.com/giantswarm/devctl/compare/v7.28.0...v7.28.1
[7.28.0]: https://github.com/giantswarm/devctl/compare/v7.27.0...v7.28.0
[7.27.0]: https://github.com/giantswarm/devctl/compare/v7.26.1...v7.27.0
[7.26.1]: https://github.com/giantswarm/devctl/compare/v7.26.0...v7.26.1
[7.26.0]: https://github.com/giantswarm/devctl/compare/v7.25.0...v7.26.0
[7.25.0]: https://github.com/giantswarm/devctl/compare/v7.24.1...v7.25.0
[7.24.1]: https://github.com/giantswarm/devctl/compare/v7.24.0...v7.24.1
[7.24.0]: https://github.com/giantswarm/devctl/compare/v7.23.2...v7.24.0
[7.23.2]: https://github.com/giantswarm/devctl/compare/v7.23.1...v7.23.2
[7.23.1]: https://github.com/giantswarm/devctl/compare/v7.23.0...v7.23.1
[7.23.0]: https://github.com/giantswarm/devctl/compare/v7.22.0...v7.23.0
[7.22.0]: https://github.com/giantswarm/devctl/compare/v7.21.0...v7.22.0
[7.21.0]: https://github.com/giantswarm/devctl/compare/v7.20.4...v7.21.0
[7.20.4]: https://github.com/giantswarm/devctl/compare/v7.20.3...v7.20.4
[7.20.3]: https://github.com/giantswarm/devctl/compare/v7.20.2...v7.20.3
[7.20.2]: https://github.com/giantswarm/devctl/compare/v7.20.1...v7.20.2
[7.20.1]: https://github.com/giantswarm/devctl/compare/v7.20.0...v7.20.1
[7.20.0]: https://github.com/giantswarm/devctl/compare/v7.19.0...v7.20.0
[7.19.0]: https://github.com/giantswarm/devctl/compare/v7.18.1...v7.19.0
[7.18.1]: https://github.com/giantswarm/devctl/compare/v7.18.0...v7.18.1
[7.18.0]: https://github.com/giantswarm/devctl/compare/v7.17.0...v7.18.0
[7.17.0]: https://github.com/giantswarm/devctl/compare/v7.16.0...v7.17.0
[7.16.0]: https://github.com/giantswarm/devctl/compare/v7.15.1...v7.16.0
[7.15.1]: https://github.com/giantswarm/devctl/compare/v7.15.0...v7.15.1
[7.15.0]: https://github.com/giantswarm/devctl/compare/v7.14.0...v7.15.0
[7.14.0]: https://github.com/giantswarm/devctl/compare/v7.13.0...v7.14.0
[7.13.0]: https://github.com/giantswarm/devctl/compare/v7.12.1...v7.13.0
[7.12.1]: https://github.com/giantswarm/devctl/compare/v7.12.0...v7.12.1
[7.12.0]: https://github.com/giantswarm/devctl/compare/v7.11.1...v7.12.0
[7.11.1]: https://github.com/giantswarm/devctl/compare/v7.11.0...v7.11.1
[7.11.0]: https://github.com/giantswarm/devctl/compare/v7.10.5...v7.11.0
[7.10.5]: https://github.com/giantswarm/devctl/compare/v7.10.4...v7.10.5
[7.10.4]: https://github.com/giantswarm/devctl/compare/v7.10.3...v7.10.4
[7.10.3]: https://github.com/giantswarm/devctl/compare/v7.10.2...v7.10.3
[7.10.2]: https://github.com/giantswarm/devctl/compare/v7.10.1...v7.10.2
[7.10.1]: https://github.com/giantswarm/devctl/compare/v7.10.0...v7.10.1
[7.10.0]: https://github.com/giantswarm/devctl/compare/v7.9.0...v7.10.0
[7.9.0]: https://github.com/giantswarm/devctl/compare/v7.8.0...v7.9.0
[7.8.0]: https://github.com/giantswarm/devctl/compare/v7.7.2...v7.8.0
[7.7.2]: https://github.com/giantswarm/devctl/compare/v7.7.1...v7.7.2
[7.7.1]: https://github.com/giantswarm/devctl/compare/v7.7.0...v7.7.1
[7.7.0]: https://github.com/giantswarm/devctl/compare/v7.6.0...v7.7.0
[7.6.0]: https://github.com/giantswarm/devctl/compare/v7.5.3...v7.6.0
[7.5.3]: https://github.com/giantswarm/devctl/compare/v7.5.2...v7.5.3
[7.5.2]: https://github.com/giantswarm/devctl/compare/v7.5.1...v7.5.2
[7.5.1]: https://github.com/giantswarm/devctl/compare/v7.5.0...v7.5.1
[7.5.0]: https://github.com/giantswarm/devctl/compare/v7.4.2...v7.5.0
[7.4.2]: https://github.com/giantswarm/devctl/compare/v7.4.1...v7.4.2
[7.4.1]: https://github.com/giantswarm/devctl/compare/v7.4.0...v7.4.1
[7.4.0]: https://github.com/giantswarm/devctl/compare/v7.3.0...v7.4.0
[7.3.0]: https://github.com/giantswarm/devctl/compare/v7.2.7...v7.3.0
[7.2.7]: https://github.com/giantswarm/devctl/compare/v7.2.6...v7.2.7
[7.2.6]: https://github.com/giantswarm/devctl/compare/v7.2.5...v7.2.6
[7.2.5]: https://github.com/giantswarm/devctl/compare/v7.2.6...v7.2.5
[7.2.6]: https://github.com/giantswarm/devctl/compare/v7.2.5...v7.2.6
[7.2.5]: https://github.com/giantswarm/devctl/compare/v7.2.4...v7.2.5
[7.2.4]: https://github.com/giantswarm/devctl/compare/v7.2.3...v7.2.4
[7.2.3]: https://github.com/giantswarm/devctl/compare/v7.2.2...v7.2.3
[7.2.2]: https://github.com/giantswarm/devctl/compare/v7.2.1...v7.2.2
[7.2.1]: https://github.com/giantswarm/devctl/compare/v7.2.0...v7.2.1
[7.2.0]: https://github.com/giantswarm/devctl/compare/v7.1.4...v7.2.0
[7.1.4]: https://github.com/giantswarm/devctl/compare/v7.1.3...v7.1.4
[7.1.3]: https://github.com/giantswarm/devctl/compare/v7.1.2...v7.1.3
[7.1.2]: https://github.com/giantswarm/devctl/compare/v7.1.1...v7.1.2
[7.1.1]: https://github.com/giantswarm/devctl/compare/v7.1.0...v7.1.1
[7.1.0]: https://github.com/giantswarm/devctl/compare/v7.0.1...v7.1.0
[7.0.1]: https://github.com/giantswarm/devctl/compare/v7.0.0...v7.0.1
[7.0.0]: https://github.com/giantswarm/devctl/compare/v6.32.0...v7.0.0
[6.32.0]: https://github.com/giantswarm/devctl/compare/v6.31.0...v6.32.0
[6.31.0]: https://github.com/giantswarm/devctl/compare/v6.30.0...v6.31.0
[6.30.0]: https://github.com/giantswarm/devctl/compare/v6.29.0...v6.30.0
[6.29.0]: https://github.com/giantswarm/devctl/compare/v6.28.0...v6.29.0
[6.28.0]: https://github.com/giantswarm/devctl/compare/v6.27.2...v6.28.0
[6.27.2]: https://github.com/giantswarm/devctl/compare/v6.27.1...v6.27.2
[6.27.1]: https://github.com/giantswarm/devctl/compare/v6.27.0...v6.27.1
[6.27.0]: https://github.com/giantswarm/devctl/compare/v6.26.4...v6.27.0
[6.26.4]: https://github.com/giantswarm/devctl/compare/v6.26.3...v6.26.4
[6.26.3]: https://github.com/giantswarm/devctl/compare/v6.26.2...v6.26.3
[6.26.2]: https://github.com/giantswarm/devctl/compare/v6.26.1...v6.26.2
[6.26.1]: https://github.com/giantswarm/devctl/compare/v6.26.0...v6.26.1
[6.26.0]: https://github.com/giantswarm/devctl/compare/v6.25.1...v6.26.0
[6.25.1]: https://github.com/giantswarm/devctl/compare/v6.25.0...v6.25.1
[6.25.0]: https://github.com/giantswarm/devctl/compare/v6.24.0...v6.25.0
[6.24.0]: https://github.com/giantswarm/devctl/compare/v6.23.3...v6.24.0
[6.23.3]: https://github.com/giantswarm/devctl/compare/v6.23.2...v6.23.3
[6.23.2]: https://github.com/giantswarm/devctl/compare/v6.23.1...v6.23.2
[6.23.1]: https://github.com/giantswarm/devctl/compare/v6.23.0...v6.23.1
[6.23.0]: https://github.com/giantswarm/devctl/compare/v6.22.0...v6.23.0
[6.22.0]: https://github.com/giantswarm/devctl/compare/v6.21.0...v6.22.0
[6.21.0]: https://github.com/giantswarm/devctl/compare/v6.20.2...v6.21.0
[6.20.2]: https://github.com/giantswarm/devctl/compare/v6.20.1...v6.20.2
[6.20.1]: https://github.com/giantswarm/devctl/compare/v6.20.0...v6.20.1
[6.20.0]: https://github.com/giantswarm/devctl/compare/v6.19.0...v6.20.0
[6.19.0]: https://github.com/giantswarm/devctl/compare/v6.18.3...v6.19.0
[6.18.3]: https://github.com/giantswarm/devctl/compare/v6.18.2...v6.18.3
[6.18.2]: https://github.com/giantswarm/devctl/compare/v6.18.1...v6.18.2
[6.18.1]: https://github.com/giantswarm/devctl/compare/v6.18.0...v6.18.1
[6.18.0]: https://github.com/giantswarm/devctl/compare/v6.17.2...v6.18.0
[6.17.2]: https://github.com/giantswarm/devctl/compare/v6.17.1...v6.17.2
[6.17.1]: https://github.com/giantswarm/devctl/compare/v6.17.0...v6.17.1
[6.17.0]: https://github.com/giantswarm/devctl/compare/v6.16.0...v6.17.0
[6.16.0]: https://github.com/giantswarm/devctl/compare/v6.15.1...v6.16.0
[6.15.1]: https://github.com/giantswarm/devctl/compare/v6.15.0...v6.15.1
[6.15.0]: https://github.com/giantswarm/devctl/compare/v6.14.0...v6.15.0
[6.14.0]: https://github.com/giantswarm/devctl/compare/v6.13.0...v6.14.0
[6.13.0]: https://github.com/giantswarm/devctl/compare/v6.12.0...v6.13.0
[6.12.0]: https://github.com/giantswarm/devctl/compare/v6.11.0...v6.12.0
[6.11.0]: https://github.com/giantswarm/devctl/compare/v6.10.0...v6.11.0
[6.10.0]: https://github.com/giantswarm/devctl/compare/v6.9.0...v6.10.0
[6.9.0]: https://github.com/giantswarm/devctl/compare/v6.8.0...v6.9.0
[6.8.0]: https://github.com/giantswarm/devctl/compare/v6.7.0...v6.8.0
[6.7.0]: https://github.com/giantswarm/devctl/compare/v6.6.0...v6.7.0
[6.6.0]: https://github.com/giantswarm/devctl/compare/v6.5.0...v6.6.0
[6.5.0]: https://github.com/giantswarm/devctl/compare/v6.4.0...v6.5.0
[6.4.0]: https://github.com/giantswarm/devctl/compare/v6.3.1...v6.4.0
[6.3.1]: https://github.com/giantswarm/devctl/compare/v6.3.0...v6.3.1
[6.3.0]: https://github.com/giantswarm/devctl/compare/v6.2.0...v6.3.0
[6.2.0]: https://github.com/giantswarm/devctl/compare/v6.1.1...v6.2.0
[6.1.1]: https://github.com/giantswarm/devctl/compare/v6.1.0...v6.1.1
[6.1.0]: https://github.com/giantswarm/devctl/compare/v6.0.0...v6.1.0
[6.0.0]: https://github.com/giantswarm/devctl/compare/v5.24.0...v6.0.0
[5.24.0]: https://github.com/giantswarm/devctl/compare/v5.23.0...v5.24.0
[5.23.0]: https://github.com/giantswarm/devctl/compare/v5.22.0...v5.23.0
[5.22.0]: https://github.com/giantswarm/devctl/compare/v5.21.1...v5.22.0
[5.21.1]: https://github.com/giantswarm/devctl/compare/v5.21.0...v5.21.1
[5.21.0]: https://github.com/giantswarm/devctl/compare/v5.20.1...v5.21.0
[5.20.1]: https://github.com/giantswarm/devctl/compare/v5.20.0...v5.20.1
[5.20.0]: https://github.com/giantswarm/devctl/compare/v5.20.0...v5.20.0
[5.20.0]: https://github.com/giantswarm/devctl/compare/v5.19.0...v5.20.0
[5.19.0]: https://github.com/giantswarm/devctl/compare/v5.18.3...v5.19.0
[5.18.3]: https://github.com/giantswarm/devctl/compare/v5.18.2...v5.18.3
[5.18.2]: https://github.com/giantswarm/devctl/compare/v5.18.1...v5.18.2
[5.18.1]: https://github.com/giantswarm/devctl/compare/v5.18.0...v5.18.1
[5.18.0]: https://github.com/giantswarm/devctl/compare/v5.17.0...v5.18.0
[5.17.0]: https://github.com/giantswarm/devctl/compare/v5.16.0...v5.17.0
[5.16.0]: https://github.com/giantswarm/devctl/compare/v5.15.0...v5.16.0
[5.15.0]: https://github.com/giantswarm/devctl/compare/v5.14.0...v5.15.0
[5.14.0]: https://github.com/giantswarm/devctl/compare/v5.13.1...v5.14.0
[5.13.1]: https://github.com/giantswarm/devctl/compare/v5.13.0...v5.13.1
[5.13.0]: https://github.com/giantswarm/devctl/compare/v5.12.0...v5.13.0
[5.12.0]: https://github.com/giantswarm/devctl/compare/v5.11.1...v5.12.0
[5.11.1]: https://github.com/giantswarm/devctl/compare/v5.11.0...v5.11.1
[5.11.0]: https://github.com/giantswarm/devctl/compare/v5.10.0...v5.11.0
[5.10.0]: https://github.com/giantswarm/devctl/compare/v5.9.0...v5.10.0
[5.9.0]: https://github.com/giantswarm/devctl/compare/v5.8.0...v5.9.0
[5.8.0]: https://github.com/giantswarm/devctl/compare/v5.7.0...v5.8.0
[5.7.0]: https://github.com/giantswarm/devctl/compare/v5.6.1...v5.7.0
[5.6.1]: https://github.com/giantswarm/devctl/compare/v5.6.0...v5.6.1
[5.6.0]: https://github.com/giantswarm/devctl/compare/v5.5.0...v5.6.0
[5.5.0]: https://github.com/giantswarm/devctl/compare/v5.4.0...v5.5.0
[5.4.0]: https://github.com/giantswarm/devctl/compare/v5.3.1...v5.4.0
[5.3.1]: https://github.com/giantswarm/devctl/compare/v5.3.0...v5.3.1
[5.3.0]: https://github.com/giantswarm/devctl/compare/v5.2.1...v5.3.0
[5.2.1]: https://github.com/giantswarm/devctl/compare/v5.2.0...v5.2.1
[5.2.0]: https://github.com/giantswarm/devctl/compare/v5.1.2...v5.2.0
[5.1.2]: https://github.com/giantswarm/devctl/compare/v5.1.1...v5.1.2
[5.1.1]: https://github.com/giantswarm/devctl/compare/v5.1.0...v5.1.1
[5.1.0]: https://github.com/giantswarm/devctl/compare/v5.0.0...v5.1.0
[5.0.0]: https://github.com/giantswarm/devctl/compare/v4.24.1...v5.0.0
[4.24.1]: https://github.com/giantswarm/devctl/compare/v4.24.0...v4.24.1
[4.24.0]: https://github.com/giantswarm/devctl/compare/v4.23.0...v4.24.0
[4.23.0]: https://github.com/giantswarm/devctl/compare/v4.22.0...v4.23.0
[4.22.0]: https://github.com/giantswarm/devctl/compare/v4.21.0...v4.22.0
[4.21.0]: https://github.com/giantswarm/devctl/compare/v4.20.1...v4.21.0
[4.20.1]: https://github.com/giantswarm/devctl/compare/v4.20.0...v4.20.1
[4.20.0]: https://github.com/giantswarm/devctl/compare/v4.19.0...v4.20.0
[4.19.0]: https://github.com/giantswarm/devctl/compare/v4.18.0...v4.19.0
[4.18.0]: https://github.com/giantswarm/devctl/compare/v4.17.0...v4.18.0
[4.17.0]: https://github.com/giantswarm/devctl/compare/v4.16.1...v4.17.0
[4.16.1]: https://github.com/giantswarm/devctl/compare/v4.16.0...v4.16.1
[4.16.0]: https://github.com/giantswarm/devctl/compare/v4.15.0...v4.16.0
[4.15.0]: https://github.com/giantswarm/devctl/compare/v4.14.0...v4.15.0
[4.14.0]: https://github.com/giantswarm/devctl/compare/v4.13.1...v4.14.0
[4.13.1]: https://github.com/giantswarm/devctl/compare/v4.13.0...v4.13.1
[4.13.0]: https://github.com/giantswarm/devctl/compare/v4.12.0...v4.13.0
[4.12.0]: https://github.com/giantswarm/devctl/compare/v4.11.0...v4.12.0
[4.11.0]: https://github.com/giantswarm/devctl/compare/v4.10.0...v4.11.0
[4.10.0]: https://github.com/giantswarm/devctl/compare/v4.9.2...v4.10.0
[4.9.2]: https://github.com/giantswarm/devctl/compare/v4.9.1...v4.9.2
[4.9.1]: https://github.com/giantswarm/devctl/compare/v4.9.0...v4.9.1
[4.9.0]: https://github.com/giantswarm/devctl/compare/v4.8.0...v4.9.0
[4.8.0]: https://github.com/giantswarm/devctl/compare/v4.7.0...v4.8.0
[4.7.0]: https://github.com/giantswarm/devctl/compare/v4.6.1...v4.7.0
[4.6.1]: https://github.com/giantswarm/devctl/compare/v4.6.0...v4.6.1
[4.6.0]: https://github.com/giantswarm/devctl/compare/v4.5.2...v4.6.0
[4.5.2]: https://github.com/giantswarm/devctl/compare/v4.5.1...v4.5.2
[4.5.1]: https://github.com/giantswarm/devctl/compare/v4.5.0...v4.5.1
[4.5.0]: https://github.com/giantswarm/devctl/compare/v4.4.0...v4.5.0
[4.4.0]: https://github.com/giantswarm/devctl/compare/v4.3.0...v4.4.0
[4.3.0]: https://github.com/giantswarm/devctl/compare/v4.2.1...v4.3.0
[4.2.1]: https://github.com/giantswarm/devctl/compare/v4.2.0...v4.2.1
[4.2.0]: https://github.com/giantswarm/devctl/compare/v4.1.0...v4.2.0
[4.1.0]: https://github.com/giantswarm/devctl/compare/v4.0.2...v4.1.0
[4.0.2]: https://github.com/giantswarm/devctl/compare/v4.0.1...v4.0.2
[4.0.1]: https://github.com/giantswarm/devctl/compare/v4.0.0...v4.0.1
[4.0.0]: https://github.com/giantswarm/devctl/compare/v3.1.0...v4.0.0
[3.1.0]: https://github.com/giantswarm/devctl/compare/v3.0.0...v3.1.0
[3.0.0]: https://github.com/giantswarm/devctl/compare/v2.0.4...v3.0.0
[2.0.4]: https://github.com/giantswarm/devctl/compare/v2.0.3...v2.0.4
[2.0.3]: https://github.com/giantswarm/devctl/compare/v2.0.2...v2.0.3
[2.0.2]: https://github.com/giantswarm/devctl/compare/v2.0.1...v2.0.2
[2.0.1]: https://github.com/giantswarm/devctl/compare/v2.0.0...v2.0.1
[2.0.0]: https://github.com/giantswarm/devctl/compare/v1.0.0...v2.0.0
[1.0.0]: https://github.com/giantswarm/devctl/releases/tag/v1.0.0
