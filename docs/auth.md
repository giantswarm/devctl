# Authentication for the agent-facing commands: `devctl auth`

The agent-facing commands (`pr wait`, `pr merge`, `release wait`) act as the engineer on GitHub and
read pipelines on CircleCI. They take their tokens from the OS keychain and from nowhere else: no
environment variable, no `gh auth token`, no file. `devctl auth login` puts the tokens there;
`devctl auth status` shows what is there. A command that finds no usable token exits 8 with one
sentence naming `devctl auth login`, before it waits for anything.

## `devctl auth login`

```nohighlight
devctl auth login                  # both flows
devctl auth login --github-only
devctl auth login --circleci-only
```

**GitHub** is the device flow of the devctl GitHub App: devctl prints the verification URL and a
code to stderr, opens the browser on the URL, and polls GitHub at the interval it names until you
have entered the code. The result is a user access token: actions are attributed to you and capped
by the App's permissions. The token expires after eight hours and comes with a refresh token valid
for six months; a command that finds the access token expired refreshes it itself and stores the new
pair, no human involved. A GitHub App used through the device flow refreshes without a client
secret, so the binary carries only the App's client id (`authstore.GitHubAppClientID`).

**CircleCI** is the OAuth 2.0 authorization code flow with PKCE (S256) and dynamic client
registration:

1. On the first login on a device, devctl registers itself as a public client (`client_name:
   devctl`, no secret, one loopback redirect URI `http://127.0.0.1:<port>/callback`) with one
   unauthenticated `POST /oauth/register`. The client id and the redirect URI stay in the keychain
   record; later logins reuse them. A new client is registered only when the record has none or
   its loopback port can no longer be bound.
2. devctl prints the authorization URL to stderr and opens the browser on it. Choose **Read**
   access on the consent page; it is all the commands need. The browser returns to the loopback
   address with the code; the state is checked.
3. devctl exchanges the code with the PKCE verifier at `POST /oauth/token`. The result is a standard
   90-day CircleCI API token that works with API v2 (`Circle-Token` header or Bearer).

CircleCI issues no refresh token: log in again before the 90 days are over. Every command warns in
its JSON `warnings` from seven days before the expiry. Re-running the flow for the same client and
user atomically revokes the previous token, so a re-login leaves no stale token behind on CircleCI.

The endpoints are the ones CircleCI publishes in its authorization server metadata
(`https://app.circleci.com/.well-known/oauth-authorization-server`): `/oauth/register`,
`/oauth/authorize`, `/oauth/token` under the issuer `https://app.circleci.com`. Sources:
[OAuth 2.0 API access with Dynamic Client Registration](https://circleci.com/docs/guides/toolkit/oauth-dynamic-client-registration/)
and the [changelog entry](https://circleci.com/changelog/oauth-2-0-api-access-with-dynamic-client-registration/).

Both tokens go into the OS keychain, service `devctl`, users `github` and `circleci`: Secret Service
on Linux, Keychain on macOS, Credential Manager on Windows. A record carries the login, the token
and its expiry, for GitHub the refresh token and its expiry, for CircleCI the client id and the
redirect URI. No command prints a token, ever; `--progress` lines and the human instructions on
stderr name logins and URLs only.

The command prints the same document as `auth status` and exits 0 when the requested flows
completed. A refused or timed-out authorization is exit 7 with the reason in the document.

## `devctl auth status`

```nohighlight
devctl auth status
```

Reads both records, contacts nothing and prints:

```json
{
  "command": "auth status",
  "schemaVersion": 1,
  "exitCode": 0,
  "verdict": "green",
  "reason": "",
  "warnings": ["CircleCI token expires in 5 day(s), at 2026-09-26T10:00:00Z: run `devctl auth login --circleci-only` before then."],
  "startedAt": "2026-09-21T10:00:00Z",
  "finishedAt": "2026-09-21T10:00:00Z",
  "github": {
    "present": true,
    "login": "octocat",
    "expiresAt": "2026-09-21T18:00:00Z",
    "expired": false,
    "refreshable": true,
    "refreshExpiresAt": "2027-03-21T10:00:00Z",
    "warnings": []
  },
  "circleci": {
    "present": true,
    "login": "octocat",
    "expiresAt": "2026-09-26T10:00:00Z",
    "expired": false,
    "refreshable": false,
    "warnings": ["CircleCI token expires in 5 day(s), at 2026-09-26T10:00:00Z: run `devctl auth login --circleci-only` before then."]
  }
}
```

Exit 0 when both identities are usable without a human (valid, or expired with a valid refresh
token); exit 8 with the `devctl auth login` invocation that fixes it when one is missing or expired
for good. An agent runs `devctl auth status || devctl auth login`.

## The gate the other commands use

`authstore.RequireGitHub(ctx)` returns the token, refreshed when needed, or `ErrAuthRequired`;
`authstore.RequireCircleCI(ctx)` returns the token or `ErrAuthRequired`, with the seven-day warning
on the token for the envelope. A CircleCI token is required only when the repository has a CircleCI
project. Both errors carry exit code 8 and the verdict `auth_required` through
`agentcli.ExitCoder`, so a command returns them unchanged.

## Exit codes and the JSON envelope

Every agent-facing command prints one JSON document on stdout when it finishes and nothing else;
`--progress` writes one line per step to stderr. The envelope is `command`, `schemaVersion`,
`exitCode`, `verdict`, `reason`, `warnings`, `startedAt`, `finishedAt`, then the command's fields.

| Code | Verdict | Meaning |
|---|---|---|
| 0 | `green`, `available` | ok |
| 1 | `red`, `ci_failed` | a check is red or the tag's CI failed |
| 2 | `timeout` | the deadline passed; the document names what was unfinished |
| 3 | `not_applicable` | draft, closed, conflicting, behind a strict base, version not resolvable |
| 4 | `required_missing` | a required context never reported |
| 5 | `refused` | another author, an opt-out |
| 7 | `usage` | wrong usage or a tooling failure |
| 8 | `auth_required` | no usable token; the reason names `devctl auth login` |

## Environment

The defaults are production; the variables exist for other sites and for tests against mocks.

| Variable | Default | Purpose |
|---|---|---|
| `DEVCTL_GITHUB_API_URL` | `https://api.github.com` | GitHub REST API |
| `DEVCTL_GITHUB_OAUTH_URL` | `https://github.com` | host of the device-flow endpoints |
| `DEVCTL_CIRCLECI_API_URL` | `https://circleci.com/api/v2` | CircleCI API v2 |
| `DEVCTL_CIRCLECI_OAUTH_URL` | `https://app.circleci.com` | CircleCI's OAuth issuer |
| `DEVCTL_REGISTRY_PUBLIC` | `gsoci.azurecr.io` | public registry, probed anonymously |
| `DEVCTL_REGISTRY_PRIVATE` | `gsociprivate.azurecr.io` | private registry, docker keychain |
| `DEVCTL_REGISTRY_INSECURE` | unset | `1` talks plain HTTP to the registries (tests) |
| `DEVCTL_KEYRING_FILE` | unset | a 0600 JSON file in place of the OS keychain (tests) |
| `DEVCTL_TIME_SCALE` | `1` | multiplies every sleep and timeout; the e2e suite runs at `0.001` |
