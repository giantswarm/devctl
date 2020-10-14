# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Include k8s dependency for 1.18 in dependabot.

### Security

- Update upload-artifact action to v2.2.0.
- Update cache action to v2.1.1.

### Fixed

- Update architect to v3.0.0 to fix the issue with updating Go module version.
  E.g.:
  https://github.com/giantswarm/operatorkit/commit/db6fafc711528b5d7474d2717cf7f4bb850f8812#diff-37aff102a57d3d7b797f152915a6dc16R1
- Update architect to v3.0.2 to allow release names to have suffixes.

## Security

- Update actions/cache actions/upload-artifact to v2.1.1 in generated
  workflows.

## [1.0.0] - 2020-09-23

### Added

 - First release.

[Unreleased]: https://github.com/giantswarm/devctl/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/giantswarm/devctl/releases/tag/v1.0.0
