# Approving Align files pull requests

`devctl pr approve-align-files [--dry-run]` processes open Align files pull requests that request your
review. `--team <team>` instead sweeps the team's repositories from `repositories/team-<team>.yaml` in
giantswarm/github, including bot-authored Align files pull requests that request no reviewer. The team
sweep can also merge an already approved, green pull request without auto-merge.

```bash
devctl pr approve-align-files --dry-run
devctl pr approve-align-files --team planeteers --dry-run
```

An Align files pull request with auto-merge normally merges itself once its required checks pass when
the repository uses the [align-files review bypass](repo.md#the-protection-step): the App
bypasses the review rule alone. Approval is unnecessary in that layout. The command remains available
for repositories that still require a review (for example, an entry with `agentMerge: false` or without
the align-files review bypass), and for the team sweep's existing branch-update and merge paths.

## Token and permissions

The command uses the [devctl GitHub App login](auth.md), unless `DEVCTL_GITHUB_TOKEN`, `GITHUB_TOKEN` or
`OPSCTL_GITHUB_TOKEN` overrides it (first set wins). It never switches identity after a refused read.
`devctl auth status` identifies the login and any override without printing a token. In CI the token
must come from an environment override; the keychain is not read.

For private repositories, the token needs access to each repository and **Pull requests: read** to
[get a pull request](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request) and
[list its reviews](https://docs.github.com/en/rest/pulls/reviews#list-reviews-for-a-pull-request).
Reading checks and commit statuses also needs **Checks: read** and **Commit statuses: read**.
Submitting an approval needs **Pull requests: write**; branch updates and team-mode merges also need
the corresponding repository write access. Team discovery needs **Contents: read** on giantswarm/github.
An App token is limited by the App's permissions and repository installation access; its user token
is limited by the person's access as well. A successful search does not prove that the same token can
read a result's pull request or reviews.

## Read failures

The status table and summary include the HTTP status and GitHub message, for example:

```text
Failed to get PR: HTTP 403 Resource not accessible by integration
Failed to get reviews: HTTP 404 Not Found
```

If every pull request fails a PR/review read with the same HTTP status, the display starts with one
batch diagnostic and the token's source, grouping identical errors with their counts instead of
repeating a row for every repository. Mixed failures and successful results retain their individual
rows. Transport and JSON decoding failures retain their explanations even without an HTTP error.

A 401 can mean an invalid or expired token; 403 can mean missing permissions or a rate limit; 404 can
mean the token cannot access a private repository. Check the actual message, identity, permissions
and App installation access before deciding which applies. A 5xx response points to a service failure.
Neither discovery nor the shared HTTP status establishes the cause on its own. A failed PR/review read
stops that pull request before approval. `--dry-run` makes no approval, branch update or merge.
