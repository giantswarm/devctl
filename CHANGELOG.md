# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [6.26.3] - 2024-05-21

### Fixed

- Handle renamed function in Apptest template

## [6.26.2] - 2024-04-25

### Fixed

- Only fetch `main` branch in GitHub actions workflow for devctl releases

## [6.26.1] - 2024-04-25

### Fixed

- Add logic to fetch the whole git history when generating the "Create Release" GitHub actions workflow file for devctl.
- Fetch whole git history for releases to fix GitHub Urls in templated file headers.

## [6.26.0] - 2024-04-24

### Fixed

- Compare Helm Rendering (only used for cluster charts): create diff comment from file to avoid size limit

## [6.25.1] - 2024-04-22

### Fixed

- Set the CI webhook secret
- Made `generate-go` Make task show up in `make help` and added a note to the readme about the template generation.

## [6.25.0] - 2024-04-18

### Added

- Add `--bumpall` flag to `release create` command to automatically bump all apps and components to the latest version.

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

- Added a new `ci-webhooks` command under the `repo setup` command that configures webhooks to our Tekton installation
- Added a new `gen apptest` command that creates the files needed by apptest-framework

## [6.21.0] - 2024-03-07

### Changed

- Compare Helm Rendering (only used for cluster charts): put matrix of diff rendering comments into single comment and upgrade dependencies

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

- Include cluster-test-catalog in "Compare Helm Rendering" action, so we can more easily test dev builds of subcharts.

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

- Fix Compare Helm Rendering action, so that, when it renders Helm with main branch code, it takes the CI test values from the main branch and not from the current branch.

## [6.17.1] - 2023-11-23

- Replace unmaintained GitHub action for release creation in "Create Release" workflow with `ncipollo/release-action`.

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
- Change `gen ami` command in order to work with aws-operator >= 14.22.0 where AMI Ids have been moved to the config repo.

### Fixed

- Fix AMI ID detection for china in `gen ami` command.

## [6.7.0] - 2023-08-18

### Changed

- Bumped Ubuntu in Github workflow runners to v22.04

## [6.6.0] - 2023-08-14

### Changed

- Exclude `.github/workflows/pre_commit_*.yaml` from renovate dependency updates, as this file is managed centrally.

## [6.5.0] - 2023-07-27

### Changed

- Updated the `update-chart` PullRequest template to include additional hints.

## [6.4.0] - 2023-06-26

### Changed

- Helm schema validation GitHub Action now skips when the `values.schema.json` file is not present for the Helm chart
  - Repositories can contain multiple Helm chart, only folders that does not have the file will be skipped and the check will be considered successful for those folders

### Removed

- Changelog: Remove `nginx-ingress-controller`. ([#595](https://github.com/giantswarm/devctl/pull/595))

## [6.3.1] - 2023-06-02

### Fixed

- Fix "Compare Helm Rendering" workflow to use correct CI values path when rendering the default branch version.

## [6.3.0] - 2023-06-01

### Added

- For flavor `cluster-app`, the make target `generate-docs` is added, to generate Markdown documentation on values.

## [6.2.0] - 2023-06-01

### Removed

- Remove reviewer from renovate file as we rely on Github `CODEOWNERS` file instead.

## [6.1.1] - 2023-05-11

### Fixed

- Fix Github action that renders Helm templates on "cluster-app" repositories, by using variables instead of hardcoding repositories names and branches.

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
- Use `GITHUB_SHA` in values validation workflow in git diff. This makes the action work with contributions from external repositories.

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

- Change Makefile target `update-deps` to only check chart dependencies with a local `Chart.yaml` in generated app Makefile template

## [5.15.0] - 2022-12-15

### Added

- Add new flavour to generate a customer workflow

### Changed

- Catch release/latest "Not Found" in workflow `create_release_pr`
- Update vendir to [v0.32.2](https://github.com/vmware-tanzu/carvel-vendir/releases/tag/v0.32.2) in update_chart workflow

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

- Add `nancy` command to Go makefile for a convenient method to run the Nancy checks the same way as they are done on the CI

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

- Update github.com/marwan-at-work/mod/cmd/mod to v0.4.2 to include fix: https://github.com/marwan-at-work/mod/pull/14

## [5.3.0] - 2022-05-10

### Changed

- Update used Go version in generated workflows to 1.18.1.
- Update `architect` to v6.4.0 (with go version 1.18.1).

## [5.2.1] - 2022-04-26

### Added
- Added file system permissions field to file generation `Input` struct. If not set, the default value remains: `0644`.
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

- Change release automation so that it automatically bumps `go.mod` module version when releasing a new major release.

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

- Creation of GitHub workflow file to validate values.schema.json if it exists for `gen workflows --flavour app`.

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

- Upgrade `fsaintjacques/semver-tool` to `3.2.0` to fix problem with releases
with high minor version.

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

- Compile binaries statically only on Linux to avoid linking issues on other
  platforms in generated Makefiles for Go.
- Fix generated Makefile for Go language for cases where there are no Go source
  files in the root module directory. In particular `make imports` is fixed.
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

- Remove `operator` flavour from `gen` commands. `app` flavour should be used
  instead.
- Remove `library` flavour from `gen` commands. `generic` flavour should be used
  instead.

## [2.0.4] - 2020-10-21

### Fixed

- Fix generated workflows warnings. E.g.
  https://github.com/kopiczko/test-gh-workflows/actions/runs/319535662
- Fix regression in "Create release branch" job in generated "Create Release"
  workflow.

## [2.0.3] - 2020-10-20

### Fixed

- Replace leftover install-tools-action with install-binary-action in generated
  workflow.

### Security

- Update actions/setup-go from v1 to v2.1.3 in generated workflows.

## [2.0.2] - 2020-10-20

### Fixed

- Replace install-tools-action with install-binary-action to break circular
  dependency between devctl and install-tools-action.

## [2.0.1] - 2020-10-16

### Fixed

- Skip generated "Create Release PR" workflow execution when release PR already
  exists.

## [2.0.0] - 2020-10-14

### Changed

- Include k8s dependency for 1.18 in generated dependabot configuration.

### Security

- Update actions/upload-artifact to v2.2.0.
- Update actions/cache to v2.1.1.

### Fixed

- Update architect to v3.0.0 to fix the issue with updating Go module version.
  E.g.:
  https://github.com/giantswarm/operatorkit/commit/db6fafc711528b5d7474d2717cf7f4bb850f8812#diff-37aff102a57d3d7b797f152915a6dc16R1
- Update architect to v3.0.2 to allow release names to have suffixes.

## [1.0.0] - 2020-09-23

### Added

 - First release.

[Unreleased]: https://github.com/giantswarm/devctl/compare/v6.26.3...HEAD
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
