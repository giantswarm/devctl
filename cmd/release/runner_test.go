package release

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func Test(t *testing.T) {
	inputChangelog := `# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Changed

- Changes pending to be released.

## [8.4.0] 2020-04-23

### Added

- Add mixed instance support for worker ASGs.

### Changed

- Improve cleanup of DrainerConfig CRs after node draining.
- Use release.Revision in Helm chart for Helm 3 support.



## [8.3.0] 2020-04-17

### Added

- Add Control Plane drainer controller.
- Add Dependabot configuration.
- Add VPC ID to AWSCluster CR status.
- Read CIDR from CR if available.

### Changed

- Drop CRD management to not ensure CRDs in operators anymore.

### Fixed

- Fix aws operator policy for latest node pools version.
- Make encryption key lookup graceful during cluster creation.



## [8.2.3] 2020-04-06

### Fixed

- Fix error handling when creating Tenant Cluster API clients.



## [8.2.2] - 2020-04-03

### Changed

- Switch from dep to Go modules.
- Use architect orb.
- Fix subnet allocation for Availability Zones.
- Switch to AWS CNI



## [8.2.1] - 2020-03-20

- Add PV limit per node. The limit is 20 PV per node.

### Added

- First release.




[Unreleased]: https://github.com/giantswarm/aws-operator/compare/v8.4.0...HEAD

[8.4.0]: https://github.com/giantswarm/aws-operator/compare/v8.3.0...v8.4.0
[8.3.0]: https://github.com/giantswarm/aws-operator/compare/v8.2.3...v8.3.0
[8.2.3]: https://github.com/giantswarm/aws-operator/compare/v8.2.2...v8.2.3
[8.2.2]: https://github.com/giantswarm/aws-operator/compare/v8.2.1...v8.2.2

[8.2.1]: https://github.com/giantswarm/aws-operator/releases/tag/v8.2.1`

	expectedChangelog := `# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

## [8.4.1] 2020-01-23

### Changed

- Changes pending to be released.

## [8.4.0] 2020-04-23

### Added

- Add mixed instance support for worker ASGs.

### Changed

- Improve cleanup of DrainerConfig CRs after node draining.
- Use release.Revision in Helm chart for Helm 3 support.



## [8.3.0] 2020-04-17

### Added

- Add Control Plane drainer controller.
- Add Dependabot configuration.
- Add VPC ID to AWSCluster CR status.
- Read CIDR from CR if available.

### Changed

- Drop CRD management to not ensure CRDs in operators anymore.

### Fixed

- Fix aws operator policy for latest node pools version.
- Make encryption key lookup graceful during cluster creation.



## [8.2.3] 2020-04-06

### Fixed

- Fix error handling when creating Tenant Cluster API clients.



## [8.2.2] - 2020-04-03

### Changed

- Switch from dep to Go modules.
- Use architect orb.
- Fix subnet allocation for Availability Zones.
- Switch to AWS CNI



## [8.2.1] - 2020-03-20

- Add PV limit per node. The limit is 20 PV per node.

### Added

- First release.




[Unreleased]: https://github.com/giantswarm/aws-operator/compare/v8.4.1...HEAD

[8.4.1]: https://github.com/giantswarm/aws-operator/compare/v8.4.0...v8.4.1
[8.4.0]: https://github.com/giantswarm/aws-operator/compare/v8.3.0...v8.4.0
[8.3.0]: https://github.com/giantswarm/aws-operator/compare/v8.2.3...v8.3.0
[8.2.3]: https://github.com/giantswarm/aws-operator/compare/v8.2.2...v8.2.3
[8.2.2]: https://github.com/giantswarm/aws-operator/compare/v8.2.1...v8.2.2

[8.2.1]: https://github.com/giantswarm/aws-operator/releases/tag/v8.2.1`

	date := time.Date(2020, 01, 23, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	outputChangelog, err := addReleaseToChangelog(inputChangelog, date, "8.4.1", "giantswarm/aws-operator")
	if err != nil {
		t.Fatal(err)
	}

	diff := cmp.Diff(expectedChangelog, outputChangelog)
	if diff != "" {
		t.Fatalf(diff)
	}
}
