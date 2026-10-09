# Authentication: `devctl auth`

devctl acts on GitHub as you, with the login of the `giantswarm-devctl` GitHub App; it reads pipelines on
CircleCI with your CircleCI token (and reruns failed workflows with it) and reaches giantswarm-repo-manager with your muster token. All three live
in the OS keychain: `devctl auth login` puts them there, `devctl auth status` shows what is there, and no
command prints a token.

An agent acts on GitHub with the same login and nothing else: `devctl auth exec` hands the App's
short-lived user token to `gh` and any other program, so a person's long-lived `gh` login stays in their
own shell and never enters an agent session.

## The GitHub token of every command

| Command | GitHub token |
|---|---|
| `auth exec` and the `gh` link | the App login; `gh` on a repository of an owner the App is not installed on keeps your own `gh` login |
| `deploy`, `pr approve-align`, `pr approve-merge-renovate`, `release create`, `release promote`, `repo create` | the App login; a token in the environment overrides it |
| the version check that precedes every command, `version check`, `version update`, `repo validate` | the same, optional and looked up only when GitHub is asked (never while the one-hour version cache is fresh): without one a public read is anonymous (`repo validate`: the embedded schema, repository names unchecked) |
| `pr wait`, `pr merge`, `pr rerun` | the App login for giantswarm; your own `gh` login (`gh auth token`) for every other owner; never a token in the environment |
| `release wait`, `rollout wait` | the App login only (`rollout wait` reads the installation with your kube context) |
| `release rerun` | none: it reads CircleCI alone |
| `ci jobs`, `ci rerun` | none: they read CircleCI alone (`ci rerun` writes with the Write login) |
| `repo setup` (and `repo setup ciwebhooks`, `repo setup renovate`), `repo checks` | your own: `$GITHUB_TOKEN` (`--github-token-envvar`) |
| `repo reconcile` | the engine's installation token in CI: `$GITHUB_TOKEN` (`--github-token-envvar`) |
| the other `repo` commands | none: giantswarm-repo-manager acts, reached with the muster token |

**The default is the App login.** Log in once with `devctl auth login --github-only`; the token refreshes
itself for six months. A command that needs a token and finds no usable login exits 8 with one sentence
naming `devctl auth login --github-only`: the document's reason for an agent-facing command, stderr for
the others.

**The App login reaches the giantswarm organization and public repositories.** The App is installed on
the giantswarm organization only. GitHub answers 404 for a private repository elsewhere, and a search
leaves it out; `deploy` and `release create` add to that 404 the cause and the variables that override
the login. `pr wait` and `pr merge` follow the owner instead, as the `gh` link does: a repository of an
owner the App is not installed on is read and merged with your own `gh` login, the document's `identity`
saying `gh`; a 404 there names the missing installation and the read access the `gh` login lacks.

**A token in the environment is an explicit override, never a fallback.** When `DEVCTL_GITHUB_TOKEN`,
`GITHUB_TOKEN` or `OPSCTL_GITHUB_TOKEN` is set (the first set one, in that order; a command with
`--github-token-envvar` reads only the variable it names), the command acts with that token and prints
one warning naming the variable, recommending `devctl auth login --github-only` and unsetting it. A token
that GitHub refuses fails the command; devctl never retries with the keychain, and never asks `gh auth
token`. `devctl auth status` shows the same warning.

**In CI** (`CI` set to any value) the keychain is never read and nothing warns: the variable is the only
source. A command that needs a token and finds none exits 8 naming the variables to set.

**The exceptions.** The App carries the agent permission set below, nothing else; the commands that need
more act with another token:

- `pr wait`, `pr merge` and `release wait` take their GitHub token from the keychain and from nowhere
  else: no environment variable, no `gh auth token`, no file. They exit 8 naming `devctl auth login`
  before they wait for anything, since they need the CircleCI token too.
- `repo setup` and its subcommands `ciwebhooks` and `renovate`, and `repo checks` need
  Administration or Webhooks write, which the App does not carry: they act with your own token, as the
  table says, and print no override warning.
- `repo reconcile` is the engine's CI path and acts with its installation token.
- `release promote` dispatches the auto-release workflow, which needs Actions write: the App carries it, so the
  login dispatches wherever you may run the workflow yourself. The document's `identity` names the token's source
  and, for the login, its account.

## The agent permission set

The `giantswarm-devctl` App's permissions are the complete set of what an agent does on GitHub. A user
token is capped twice: by these permissions and by the person's own rights, so the App gives nobody more
than they already have. GitHub attributes every action to the person and records the App in the audit
log on every request, which separates an agent's actions from the person's own.

| Agent action | Permission |
|---|---|
| Read repositories, pull requests, checks, statuses, workflow runs and logs, issues | metadata, contents, checks, statuses, actions: read |
| Comment on issues and pull requests, open and edit issues | issues, pull requests: write |
| Open or update a pull request | pull requests: write |
| Push a branch, `.github/workflows` included | contents, workflows: write |
| Merge through the ruleset bypass (`pr merge`): GitHub evaluates the merge as the person, so the owning team's or the admins' bypass applies | contents, pull requests: write |
| Release: dispatch a workflow (`release promote`, `pr merge --dispatch`); the tag itself comes from the auto-release workflow's own token | actions, contents: write |
| Approve and re-run workflow runs | actions: write |
| Write the organization's project boards (Projects v2) | organization projects: write |

**Not an agent action:** administration (branch protection, rulesets, settings, webhooks, deploy keys,
repository creation). It stays with giantswarm-repo-manager, its own App reached through the `repo`
commands, or with a person in their own shell.

The App is installed on the giantswarm organization only. `gh` through devctl therefore acts with the
App token for giantswarm's repositories and wherever no repository is named, and keeps the person's own
`gh` login for a repository of another owner, which the App cannot reach (below).

## `devctl auth exec`

```nohighlight
devctl auth exec -- gh api user
devctl auth exec -- gh pr comment 42 --repo giantswarm/devctl --body "..."
```

Runs a command with the App login's token in `$GH_TOKEN`, which `gh` reads in preference to its own
login. The token is the keychain's, refreshed first when it expires within ten minutes, so a command never
starts on a token that dies under it; devctl hands it to the command's environment and never prints it.
devctl replaces itself with the command: the exit code, signals and terminal are the command's. Without a
usable login it exits 8 with one line on stderr naming `devctl auth login --github-only`; a command not
found is exit 7. Environment variables do not override the keychain here, and no version check precedes
it: an outdated devctl still hands over the token, so a devctl release never stops an agent's `gh`.

**The `gh` link.** devctl started under the name `gh` (a link to it) is `devctl auth exec -- gh`: it runs
the next `gh` on `PATH` that is not devctl itself. An agent environment puts such a link in a directory
first on the agent's `PATH`:

```nohighlight
mkdir -p ~/.local/share/devctl/agent-bin
ln -s "$(command -v devctl)" ~/.local/share/devctl/agent-bin/gh
# in the agent's shell only, never the person's own:
export PATH="$HOME/.local/share/devctl/agent-bin:$PATH"
```

**The owner rule.** `gh` acting on a repository of an owner the App is not installed on runs without the
token, on the person's own `gh` login: the App does not reach that repository. The repository is the one
gh acts on: `--repo`/`-R`, `$GH_REPO`, a `repos/<owner>/…` or `orgs/<owner>/…` endpoint of `gh api`, the
URL or repository argument of `gh pr|issue|run|release|repo|workflow`, else the working directory's
repository as gh resolves it (`gh repo set-default`, then the remotes `upstream`, `github`, `origin`).
Everything else (a giantswarm repository, `gh api user`, `gh api graphql`, `gh search`) acts with the App
token.

Every `gh` the agent runs, also from scripts and subprocesses, then acts by that rule, and
`devctl version update` replacing the binary keeps the link working. `gh auth status` in such a shell
names `GH_TOKEN` as the source; `gh api user/installations` answers only for an App user token and names
`giantswarm-devctl`. `git` over SSH keeps its SSH key; a `git` whose credential helper is `gh auth
git-credential` resolved through `PATH` uses the App token too.

## `devctl auth login`

```nohighlight
devctl auth login                  # GitHub and CircleCI
devctl auth login --github-only
devctl auth login --circleci-only
devctl auth login --muster-only    # muster, for the repo commands
```

**GitHub** is the device flow of the `giantswarm-devctl` GitHub App, owned by the `giantswarm`
organization: devctl prints the verification URL and a code to stderr, opens the browser on the URL, and polls GitHub at the interval it names until you
have entered the code. The result is a user access token: actions are attributed to you and capped
by the App's permissions. The token expires after eight hours and comes with a refresh token valid
for six months; a command that finds the access token expired refreshes it itself and stores the new
pair, no human involved. A long run (`pr wait`, `pr merge`, `release wait`) outlives the eight hours:
when GitHub answers 401 to its token, it renews the token and sends the refused request once more,
and the wait goes on from where it was. The renewal reads the keychain again first, because another
devctl may have refreshed the pair meanwhile and GitHub accepts only the newest one; a lock beside
the keychain (a file in the user's cache directory) makes every devctl on the machine spend a refresh
token once. The run exits 8 only when the refresh itself is refused, and a 401 to the renewed token
is exit 7 with GitHub's answer. A GitHub App used through the device flow refreshes without a client
secret, so the binary carries only the App's client id (`authstore.GitHubAppClientID`,
`Iv23liWio5REm4MfY2Mw`); a client id is public, embedding it discloses nothing.

**CircleCI** is the OAuth 2.0 authorization code flow with PKCE (S256) and dynamic client
registration:

1. On the first login on a device, devctl registers itself as a public client (`client_name:
   devctl`, no secret, one loopback redirect URI `http://127.0.0.1:<port>/callback`) with one
   unauthenticated `POST /oauth/register`. The client id and the redirect URI stay in the keychain
   record; later logins reuse them. A new client is registered only when the record has none or
   its loopback port can no longer be bound.
2. devctl prints the authorization URL to stderr and opens the browser on it. Choose **Write**
   access on the consent page: `pr rerun` and `release rerun` rerun workflows, a write; every other
   command reads. A login that granted Read access keeps working for the reads, and a rerun on it is
   exit 8 naming `devctl auth login --circleci-only`. The browser returns to the loopback address
   with the code; the state is checked.
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

**muster** (`--muster-only`) is the sign-in to the muster MCP endpoint that runs
giantswarm-repo-manager, for the `repo` commands: `--muster-endpoint`, `$DEVCTL_MUSTER_URL` or the
default, gazelle's `https://muster.gazelle.awsprod.gigantic.io/mcp`. muster is its own OAuth 2.1
authorization server, so the flow is discovered, not configured:

1. devctl reads the endpoint's protected resource metadata (RFC 9728, the path-inserted well-known
   URL first) for the authorization server, then the server's metadata (RFC 8414) for its
   endpoints; a server without S256 PKCE is refused.
2. devctl registers no client: muster gates its registration endpoint with a token a CLI on every
   laptop cannot carry. Its `client_id` is a Client ID Metadata Document, the way the muster agent
   identifies itself -- `https://giantswarm.github.io/muster/devctl.json`, served by the muster
   repository's GitHub Pages, naming the public client devctl (no secret), the loopback redirect
   URIs (any port, RFC 8252 §7.3), the authorization code and refresh token grants and the scopes;
   the server fetches it once. A server whose metadata does not advertise
   `client_id_metadata_document_supported` is refused. The redirect URI, the endpoint and the issuer
   stay in the keychain record.
3. devctl prints the authorization URL to stderr and opens the browser on it; the scopes are the
   ones the endpoint's metadata names, else `openid profile email groups offline_access`, and the
   token is bound to the endpoint with the `resource` indicator (RFC 8707). The browser returns to
   the loopback address with the code; the state is checked; devctl exchanges the code with the
   PKCE verifier and reads the login from the userinfo endpoint.
4. The access token comes with a refresh token (the `offline_access` scope): a `repo` command that
   finds the access token expired refreshes it itself, no human involved, and stores the new pair.

Then the sign-in to giantswarm-repo-manager, once: the manager is pinned to its own GitHub App, so
devctl asks muster (`core_auth_login`) for the sign-in, prints and opens the App's consent URL when
one is needed, and asks the manager who is calling (`get_info`) until it answers -- that is when the
consent is filed under you in muster (ten minutes at most). The document names the outcome under
`giantswarmRepoManager` (`signedIn`, `caller`). A muster that does not run the manager (a lab's,
an installation's own) is a note there and a warning, not a failure: the muster sign-in stands.

The tokens go into the OS keychain, service `devctl`, users `github`, `circleci` and `muster`:
Secret Service on Linux, Keychain on macOS, Credential Manager on Windows. A record carries the
login, the token and its expiry, for GitHub and muster the refresh token (and its expiry, when the
server names one), for CircleCI and muster the client id and the redirect URI, for muster the
endpoint and the issuer. No command prints a token, ever; `--progress` lines and the human
instructions on stderr name logins and URLs only.

The command prints the same document as `auth status` and exits 0 when the requested flows
completed. A refused or timed-out authorization is exit 7 with the reason in the document.

## `devctl auth status`

```nohighlight
devctl auth status
```

Reads the records, contacts nothing and prints:

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

A GitHub token in `DEVCTL_GITHUB_TOKEN`, `GITHUB_TOKEN` or `OPSCTL_GITHUB_TOKEN` adds the override warning to
`warnings` and `github.warnings`, never the token and never with `CI` set; the exit code stays the keychain's.

Exit 0 when the GitHub and CircleCI identities are usable without a human (valid, or expired with
a valid refresh token); exit 8 with the `devctl auth login` invocation that fixes it when one is
missing or expired for good. An agent runs `devctl auth status || devctl auth login`. The `muster`
identity is the third block of the document, with its `endpoint`, and is reported, not required:
only the `repo` commands need it, and they exit 8 naming `devctl auth login --muster-only`
themselves.

## The gate the commands use

`authstore.ResolveGitHub(ctx, envVars...)` is the GitHub token of the commands for people, by the rules
above: `envVars` are the variables to read, none meaning `authstore.GitHubEnvVars` (`DEVCTL_GITHUB_TOKEN`,
`GITHUB_TOKEN`, `OPSCTL_GITHUB_TOKEN`), a command with `--github-token-envvar` passing its one name. The
token's `Source` says where it came from (`keychain` or `$NAME`) and its `Warning` is the override notice
to print once, empty for the App login and in CI; the error is `ErrAuthRequired`, returned unchanged, and
`devctl` exits 8 with it. `authstore.GitHubOverrideWarning(envVars...)` is that warning alone.

`authstore.RequireGitHub(ctx)` returns the App login, refreshed when needed, or `ErrAuthRequired`;
`authstore.RequireCircleCI(ctx)` returns the token or `ErrAuthRequired`, with the seven-day warning
on the token for the envelope. A CircleCI token is required only when the repository has a CircleCI
project. `authstore.RequireMuster(ctx)` returns the muster token, refreshed when needed, with the
endpoint it is for, or `ErrAuthRequired` naming `--muster-only`. The errors carry exit code 8 and
the verdict `auth_required` through `agentcli.ExitCoder`, so a command returns them unchanged.

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
| 4 | `required_missing`, `approval_required` | a required context never reported; Actions runs await a member's approval |
| 5 | `refused` | another author, an opt-out |
| 7 | `usage` | wrong usage, a newer devctl released (the reason names `devctl version update`), or a tooling failure |
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
| `DEVCTL_MUSTER_URL` | `https://muster.gazelle.awsprod.gigantic.io/mcp` | the muster MCP endpoint `--muster-only` signs in to and the `repo` commands reach giantswarm-repo-manager through |
| `DEVCTL_KEYRING_FILE` | unset | a 0600 JSON file in place of the OS keychain (tests) |
| `DEVCTL_TIME_SCALE` | `1` | multiplies every sleep and timeout; the e2e suite runs at `0.001` |
