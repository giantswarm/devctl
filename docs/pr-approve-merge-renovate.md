# PR Approve Merge Renovate Command

## Overview

The `devctl pr approve-merge-renovate` command automates the approval and merging of Renovate-generated pull requests across multiple repositories.

## Usage

```bash
devctl pr approve-merge-renovate "architect v6.7.0"
```

## Arguments

- `<query>` (required): Search query to filter Renovate PRs

## Options

- `--dry-run`: Show what would be done without making changes
- `--merge-method`: Override merge method (merge, squash, rebase). If not set, uses repository's default merge method.

## How It Works

1. **Search**: Searches for PRs matching the query with these filters:
   - `is:pr is:open`
   - `archived:false`
   - `review-requested:@me`
   - `author:app/renovate`

2. **Parallel Processing**: All PRs are processed simultaneously using goroutines for maximum speed

3. **Continuous Polling**: Every 10 seconds, re-runs the search query to discover newly created PRs that match the criteria and automatically processes them

4. **Live Table UI**: Displays a real-time updating table showing:
   - PR number (as a clickable hyperlink)
   - Repository name
   - Current status with icons

5. **For Each PR**:
   - Checks if already merged (skip if yes)
   - Checks if auto-merge is enabled (skip if yes)
   - Verifies status checks are passing
   - **Polls and waits** if checks are pending (retries up to 60 times over 5 minutes)
   - Approves when checks pass (if not already approved)
   - Determines appropriate merge method from repository settings
   - Merges the PR

6. **Auto-retry Logic**: 
   - PRs with pending checks are automatically polled every 5 seconds
   - Once checks pass, they're immediately approved and merged
   - No manual intervention needed

7. **Summary**: Displays final statistics about:
   - PRs merged
   - PRs approved
   - PRs skipped
   - PRs failed

## Examples

### Approve and merge all Renovate PRs for a specific dependency update

```bash
devctl pr approve-merge-renovate "architect v6.7.0"
```

### Preview what would happen without making changes

```bash
devctl pr approve-merge-renovate "Update Go to v1.21" --dry-run
```

### Override the merge method (only if allowed in the repository)

```bash
devctl pr approve-merge-renovate "renovate dependency" --merge-method rebase
```

**Note**: If the specified merge method is not allowed in a repository, that PR will be skipped with a warning.

## Requirements

- `GITHUB_TOKEN` environment variable must be set with appropriate permissions:
  - Read access to repositories
  - Write access to pull requests (approve, merge)
- Terminal with ANSI escape code support for live table updates
- Terminal with OSC 8 support for clickable hyperlinks (optional, but recommended)

## UI Features

The command displays a live-updating table with status indicators:

- **🔍** Checking status
- **⏳** Waiting for checks to pass (with retry count)
- **☑️** Already handled (merged or auto-merge enabled)
- **✅** Successfully approved or merged
- **❌** Failed or skipped

PR numbers are clickable hyperlinks (in supported terminals) that open the PR in your browser.

## Performance

- **Parallel Processing**: All PRs are processed simultaneously
- **No waiting**: You don't need to wait for one PR to finish before the next starts
- **Auto-retry**: PRs with pending checks are automatically retried until ready
- **Continuous Discovery**: New PRs matching the query are automatically detected every 10 seconds
- Example: 13 PRs can be processed in the time it takes for the slowest one to become ready

## Notes

- The command processes PRs in the `giantswarm` organization
- PRs with pending checks are automatically polled every 5 seconds for up to 5 minutes
- PRs with merge conflicts or permanently failed checks will be skipped
- Auto-merge enabled PRs are skipped (no need to merge manually)

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

