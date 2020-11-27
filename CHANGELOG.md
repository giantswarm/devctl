# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## Added

- Add Dockerfile.
- Generate main Makefile including `*.mk` files to allow custom Makefiles.
- Pretty print errors.
- Print devctl version in generated files headers.

## Changed

- Generate language specific Makefiles in `gen makefile`.
- Add required `--language` flag to `gen makefile`.

## Fixed

- Fix Azure tag URL in release changelog generation.

## Removed

- Remove `repo list` command replaced with Go modules + dependabot.

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

[Unreleased]: https://github.com/giantswarm/devctl/compare/v3.1.0...HEAD
[3.1.0]: https://github.com/giantswarm/devctl/compare/v3.0.0...v3.1.0
[3.0.0]: https://github.com/giantswarm/devctl/compare/v2.0.4...v3.0.0
[2.0.4]: https://github.com/giantswarm/devctl/compare/v2.0.3...v2.0.4
[2.0.3]: https://github.com/giantswarm/devctl/compare/v2.0.2...v2.0.3
[2.0.2]: https://github.com/giantswarm/devctl/compare/v2.0.1...v2.0.2
[2.0.1]: https://github.com/giantswarm/devctl/compare/v2.0.0...v2.0.1
[2.0.0]: https://github.com/giantswarm/devctl/compare/v1.0.0...v2.0.0
[1.0.0]: https://github.com/giantswarm/devctl/releases/tag/v1.0.0
