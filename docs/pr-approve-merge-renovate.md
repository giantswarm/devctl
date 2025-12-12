# PR Approve Merge Renovate Command

## Overview

The `devctl pr approve-merge-renovate` command automates the approval and merging of Renovate-generated pull requests across multiple repositories. It continuously monitors for new PRs matching your query, processes them in parallel, and handles both auto-merge enabled PRs and direct merging.

## Usage

```bash
devctl pr approve-merge-renovate QUERY
```

Example:

```bash
devctl pr approve-merge-renovate "architect v1.2.3"
```

## Arguments

- `<query>` (required): Search query to filter Renovate PRs

## Options

- `--dry-run`: Show what would be done without making changes
- `--watch`, `-w`: Keep running and continuously watch for new PRs (polls every minute, exit with Ctrl+C)

## How It Works

1. **Search**: Searches for PRs matching the query with these filters:
   - `is:pr is:open`
   - `archived:false`
   - `review-requested:@me`
   - `author:app/renovate`

2. **Parallel Processing**: All PRs are processed simultaneously using goroutines for maximum speed

3. **Continuous Polling**: Re-runs the search query to discover newly created PRs that match the criteria and automatically processes them
   - Normal mode: Polls every 10 seconds until all PRs are processed
   - Watch mode (`--watch`): Polls every minute and runs indefinitely

4. **Live Table UI**: Displays a real-time updating table showing:
   - PR number (as a clickable hyperlink in supported terminals)
   - Repository name
   - Current status (text-based, no emojis)

5. **For Each PR**:
   - Checks if already merged (skip if yes)
   - Verifies status checks are passing (reports "Failed checks" if failing)
   - **Polls and waits** if checks are pending (retries up to 60 times over 5 minutes)
   - Checks if auto-merge is enabled after checks pass
   - Approves the PR when checks pass (if not already approved)
   - **If auto-merge enabled**: Waits up to 1 minute for auto-merge to complete
   - **If no auto-merge**: Determines merge method from repository settings and merges directly

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
devctl pr approve-merge-renovate "architect v1.2.3"
```

### Preview what would happen without making changes

```bash
devctl pr approve-merge-renovate "helmclient v4.12.7" --dry-run
```

### Process PRs matching a partial string

```bash
devctl pr approve-merge-renovate "helm v3"
```

This will match any Renovate PRs with "helm v3" in the title (e.g., "Update helm to v3.15.0", "Update helm to v3.16.0").

### Watch mode - continuously monitor for new PRs

```bash
devctl pr approve-merge-renovate "architect v1" --watch
```

In watch mode:
- The command keeps running after all current PRs are processed
- Polls for new matching PRs every minute (instead of every 10 seconds)
- Automatically processes any new PRs that appear
- Exit with Ctrl+C (or Cmd+C on macOS)

This is useful when you expect multiple Renovate PRs to be created over time.

## Requirements

- `GITHUB_TOKEN` environment variable must be set with appropriate permissions:
  - Read access to repositories
  - Write access to pull requests (approve, merge)
- Terminal with ANSI escape code support for live table updates
- Terminal with OSC 8 support for clickable hyperlinks (optional, but recommended)

## UI Features

The command displays a live-updating table with text-based status messages:

**Common Status Messages:**
- `Checking...` - Retrieving PR information
- `Failed checks` - PR has failing status checks (skipped)
- `Waiting for checks (N/60)` - Polling until checks pass
- `Approving...` - Approving the PR
- `Approved` - PR approved, ready to merge
- `Approved (auto-merge)` - PR approved, waiting for auto-merge
- `Auto-merge enabled` - PR already approved with auto-merge
- `Merged (squash/merge/rebase)` - PR successfully merged
- `Would approve (auto-merge)` - Dry-run: would approve auto-merge PR
- `Would approve & merge` - Dry-run: would approve and merge PR
- `Already merged` - PR was already merged

**Table Format:**
```
PR      Repository                               Status
────────────────────────────────────────────────────────────────────────────────
#442    app-build-suite                          Failed checks
#638    apptestctl                               Merged (squash)
```

PR numbers are clickable hyperlinks (in supported terminals) that open the PR in your browser.

## Performance

- **Parallel Processing**: All PRs are processed simultaneously
- **No waiting**: You don't need to wait for one PR to finish before the next starts
- **Auto-retry**: PRs with pending checks are automatically retried until ready
- **Continuous Discovery**: 
  - Normal mode: New PRs detected every 10 seconds
  - Watch mode: New PRs detected every minute
- **Example**: 13 PRs can be processed in the time it takes for the slowest one to become ready

## Watch Mode Use Cases

Watch mode (`--watch`) is particularly useful for:
- **Mass dependency updates**: When Renovate creates many PRs for the same update across multiple repositories
- **Gradual rollouts**: When PRs are created over time as different repositories become eligible
- **Long-running automation**: Keep the command running in CI/CD or as a background process
- **Batch operations**: Process all PRs of a certain type as they appear without manual intervention

## Notes

- The command searches for PRs with `review-requested:@me` and `author:app/renovate` in any organization
- PRs are displayed with only the repository name (owner prefix removed for cleaner display)
- PRs with pending checks are automatically polled every 5 seconds for up to 5 minutes
- PRs with failed checks are skipped and reported as "Failed checks"
- PRs with merge conflicts are skipped with error message
- **Auto-merge behavior**:
  - PRs with auto-merge enabled and failing checks: Show "Failed checks"
  - PRs with auto-merge enabled and passing checks: Approve and wait up to 1 minute for auto-merge
  - PRs without auto-merge: Approve and merge directly using repository's default merge method
- **Normal mode**: New PRs are discovered every 10 seconds; command exits when all PRs are processed
- **Watch mode (`--watch`)**: 
  - New PRs are discovered every minute
  - Command runs indefinitely, never exits automatically
  - Perfect for long-running Renovate batch updates
  - Exit gracefully with Ctrl+C or Cmd+C
