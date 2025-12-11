# PR Approve Merge Renovate Command

## Overview

The `devctl pr approve-merge-renovate` command automates the approval and merging of Renovate-generated pull requests across multiple repositories.

## Usage

```bash
devctl pr approve-merge-renovate --query "architect v6.7.0"
```

## Options

- `--query, -q` (required): Search query to filter Renovate PRs
- `--dry-run`: Show what would be done without making changes
- `--merge-method`: Override merge method (merge, squash, rebase). If not set, uses repository's default merge method.

## How It Works

1. **Search**: Searches for PRs matching the query with these filters:
   - `is:pr is:open`
   - `archived:false`
   - `review-requested:@me`
   - `author:app/renovate`

2. **Check Status**: For each PR found:
   - Verifies status checks are passing
   - Skips PRs with failed checks

3. **Check Auto-merge**: 
   - Skips PRs that already have auto-merge enabled
   - Only merges PRs without auto-merge to avoid duplicate actions

4. **Approve**: If not already approved:
   - Approves the PR

5. **Determine Merge Method**:
   - If `--merge-method` is specified, validates it's allowed in the repository
   - Otherwise, uses repository's default (prefers squash > merge > rebase)

6. **Merge**: If not already merged:
   - Attempts to merge the PR using the determined merge method

7. **Summary**: Displays statistics about:
   - PRs approved
   - PRs merged
   - PRs skipped

## Examples

### Approve and merge all Renovate PRs for a specific dependency update

```bash
devctl pr approve-merge-renovate --query "architect v6.7.0"
```

### Preview what would happen without making changes

```bash
devctl pr approve-merge-renovate --query "Update Go to v1.21" --dry-run
```

### Override the merge method (only if allowed in the repository)

```bash
devctl pr approve-merge-renovate --query "renovate dependency" --merge-method rebase
```

**Note**: If the specified merge method is not allowed in a repository, that PR will be skipped with a warning.

## Requirements

- `GITHUB_TOKEN` environment variable must be set with appropriate permissions:
  - Read access to repositories
  - Write access to pull requests (approve, merge)

## Notes

- The command processes PRs in the `giantswarm` organization
- PRs must have passing status checks to be processed
- PRs with merge conflicts or missing required checks will be skipped
- Auto-merge is attempted immediately after approval

## Comparison with `pr approvealign`

| Feature | `approvealign` | `approve-merge-renovate` |
|---------|----------------|--------------------------|
| Scope | "Align files" PRs | Renovate PRs with custom query |
| Query | Fixed | User-specified |
| Approve | ✅ | ✅ |
| Merge | ❌ | ✅ |
| Dry-run | ❌ | ✅ |
| Merge method | N/A | Configurable |

## Implementation

The command is implemented in `cmd/pr/approvemergerenovate/` with the following files:

- `command.go`: Command definition and configuration
- `runner.go`: Main implementation logic
- `flag.go`: Command-line flag definitions and validation
- `error.go`: Error definitions

