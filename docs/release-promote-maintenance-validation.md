# Maintenance-ref promotion acceptance proof

The read-only acceptance criterion of [the maintenance-ref issue](https://github.com/giantswarm/devctl/issues/2695) passed on 2026-11-06 with source commit `6404e2b4a082a254622766cb0cee9fc473c24cde` of [the implementation PR](https://github.com/giantswarm/devctl/pull/3159).

Built that clean checkout with `go build -p 2 -o devctl-proof .` under `beekeeper run`. Go build metadata reports `vcs.revision=6404e2b4a082a254622766cb0cee9fc473c24cde` and `vcs.modified=false`. The local pseudo-version predates released devctl 8.164.2, so the version check initially returned usage exit 7. Its documented, exact-version override was used solely to exercise this reviewed build:

```sh
DEVCTL_UNSAFE_FORCE_VERSION=8.164.2-0.20261106153735-6404e2b4a082 \
  ./devctl-proof release promote giantswarm/substrate --ref release-1.9 --dry-run
```

The command used the existing keychain login and live GitHub endpoints, without fixture clients or endpoint overrides. It exited 0 with this complete JSON document:

```json
{
  "command": "release promote",
  "schemaVersion": 1,
  "exitCode": 0,
  "verdict": "green",
  "reason": "",
  "warnings": [],
  "startedAt": "2026-11-06T17:28:16.700689431Z",
  "finishedAt": "2026-11-06T17:28:57.334931498Z",
  "team": "",
  "dryRun": true,
  "identity": {
    "source": "keychain",
    "login": "teemow"
  },
  "repositories": [
    {
      "repository": "giantswarm/substrate",
      "stable": "v1.9.1",
      "candidate": "v1.9.2-rc.1",
      "statusState": "success",
      "state": "would_dispatch",
      "message": "would dispatch zz_generated.auto_release.yaml on release-1.9 to promote v1.9.2-rc.1"
    }
  ]
}
```

## Independent live selection evidence

Read all three pages of [GitHub releases](https://api.github.com/repos/giantswarm/substrate/releases?per_page=100) with `gh api --paginate --slurp`: 206 releases, including these 12 matching `v1.9.Z` or `v1.9.Z-rc.N`. None is a draft; all RCs are pre-releases and both stable releases are full releases.

| Patch | Stable release | Published candidates |
| --- | --- | --- |
| 2 | none | `v1.9.2-rc.1` |
| 1 | `v1.9.1` | `v1.9.1-rc.1` through `v1.9.1-rc.6` |
| 0 | `v1.9.0` | `v1.9.0-rc.1` through `v1.9.0-rc.3` |

The [live release-1.9 branch](https://api.github.com/repos/giantswarm/substrate/branches/release-1.9) was at `46311898ee228071adaab80209fd35270c9306a2`. The [candidate comparison](https://github.com/giantswarm/substrate/compare/v1.9.2-rc.1...release-1.9) returned `status: identical`, `ahead_by: 0`, `behind_by: 0`; its base and merge-base were that same SHA. Thus the highest published 1.9 RC is reachable and exactly the branch head. The [stable comparison](https://github.com/giantswarm/substrate/compare/v1.9.1...release-1.9) returned `status: ahead`, `ahead_by: 2`, `behind_by: 0`, with base and merge-base `4bede2419a5debac90e0d79ab5242fc8c676db24`. This establishes the highest 1.9 stable baseline is reachable too.

The [workflow at the selected ref](https://github.com/giantswarm/substrate/blob/release-1.9/.github/workflows/zz_generated.auto_release.yaml) declares push branches `giantswarm` and `release-[0-9]+.[0-9]+`, plus `workflow_dispatch` accepting `release-type: stable`. The command reports successful candidate commit status and names `release-1.9` as the intended dispatch ref. No stable dispatch was performed.

This repair adds evidence only; production and test code are unchanged from the proven source commit. Existing exact-head CI and regression coverage establish the implementation checks; this live run establishes the previously missing acceptance criterion.
