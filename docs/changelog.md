# Cutting the CHANGELOG (`devctl changelog`)

Auto Release tags a release from every releasable merge. The entries the merged pull request added under
`## [Unreleased]` in CHANGELOG.md are part of that release, and the CHANGELOG.md of the tag still lists them under
Unreleased. Left alone, Unreleased collects every released change and reads as if none had shipped.

```
devctl changelog cut   [--file CHANGELOG.md]
devctl changelog check [--file CHANGELOG.md] [--base <revision>]
```

Both run in a clone with its tags (a shallow clone or one fetched without tags sees no release), from the directory
`--file` is relative to.

## Which release carried an entry

The releases are the `v` semver tags reachable from HEAD; the tags on one commit are one release, named by the highest
version, so a promoted release candidate is named by its final version. An entry is released by the first release, in
version order, whose CHANGELOG.md holds it under Unreleased. An entry is its bullet line and every line continuing it,
blank lines and fenced blocks included, compared without trailing white space. An entry written again after a release
carried it is credited once per copy: a release is credited only with the copies its CHANGELOG.md holds beyond the
previous release's, and the copy no release carried stays under Unreleased.

## `cut`

Moves every released entry into its release's section, in its own group (`### Added`, `### Fixed`, …, in Keep a
Changelog's order). A missing section is created as `## [<version>] - <date of the tagged commit>` in version order,
with its link reference (`[<version>]: …/compare/<older tag>...<tag>`) when the file keeps link references, and the
`[Unreleased]` reference then compares from the newest release. Unreleased and the sections receiving entries are
rewritten; every other section, the preamble and the existing link references keep their bytes. A second cut changes
nothing. A file without an Unreleased section is left alone.

## `check`

Exits 0 when Unreleased holds only changes no release carries, and non-zero listing each released entry with the tag
that carried it.

`--base <revision>` checks a pull request on its merge with its base: an entry the base's CHANGELOG.md already holds
released under Unreleased is listed as waiting for the base's own cut pull request and does not fail the check. A
release cut on the base therefore does not turn the check red on every open pull request until its cut merges; an
entry the pull request itself brings back under Unreleased still fails it.

## In the generated workflows

On the `auto-release` release workflow (`devctl gen workflows --release-workflow=auto-release`):

- `zz_generated.auto_release.yaml` has a job `changelog` that runs after every release the tag job cut. It runs
  `devctl changelog cut` on the head of the released branch and, when the cut changed CHANGELOG.md, pushes it to the
  branch `changelog-cut/<branch>`, opens the pull request `docs(changelog): cut released entries out of Unreleased`
  against the released branch (or updates the open one) and arms auto-merge on it. git-cliff skips `docs`, so the
  merge cuts no release of its own. The pull request is opened with `TAYLORBOT_GITHUB_ACTION`, as a pull request
  opened with `GITHUB_TOKEN` runs no workflow and its required checks would never report; without the secret the job
  warns and opens nothing. The review the branch's ruleset requires comes from a second identity: the job approves the
  pull request with `ARCHITECT_GITHUB_REPO_SYNC_TOKEN` (architectbot), since an author cannot approve its own, so the
  armed auto-merge lands the cut once its required checks are green. It approves only the cut it pushed: a pull request
  the opening identity authored, whose head is the cut commit and whose only file is `CHANGELOG.md`; anything else
  waits for a review. The auto-merge and the approval are pinned to that commit, so a later push to the branch
  merges nothing on them. Without that secret, or where the approval or
  auto-merge is refused, the job warns and the pull request waits for a review or a merge.
- `zz_generated.changelog.yaml` runs `devctl changelog check --base <the pull request's base commit>` on every pull
  request ("No released entries under Unreleased"): released entries the base already holds wait for its open cut
  pull request and pass; for the pull request's own, merging the open cut pull request, or running
  `devctl changelog cut` and committing CHANGELOG.md, clears it. A repository without CHANGELOG.md passes.

Both download the latest devctl release. A fork line carries upstream's CHANGELOG.md and gets neither; the `legacy`
release workflow writes the CHANGELOG.md sections in its release pull request, and its run deletes
`zz_generated.changelog.yaml`.
