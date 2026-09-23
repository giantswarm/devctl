# End-to-end tests: devctl against mocked GitHub, CircleCI and registry

The agent commands (`auth login`, `pr wait`, `pr merge`, `release wait`) encode what our CI, CircleCI,
auto-release and the registry do in the field: a stage gap that reads as green, a fork's workflow run awaiting
approval, a renamed image, a rerun that keeps a failed workflow, a stale registry login. Each of those is a
scenario here, run on every pull request: the built `devctl` binary against in-process mocks of the GitHub REST
API, the CircleCI API v2 and an OCI registry, with time under test control. A new field report becomes a new
scenario before the fix.

## How a run works

`go test ./e2e/...` is part of `make test`. `TestMain` builds `devctl` once (`go build`, version `0.0.0-e2e`);
every directory under `scenarios/` that holds a `scenario.yaml` is one subtest, and the subtests run in
parallel. A scenario gets its own mock servers on loopback ports, a home directory of its own and an
environment built from scratch (below); the binary runs with the scenario directory as its working directory, so
a fixture file beside `scenario.yaml` is referenced by its bare name. The run is bounded by the scenario's
`timeout` (60 s by default); `DEVCTL_TIME_SCALE=0.001` makes a thirty-minute wait take 1.8 seconds, and the
mocks answer in microseconds, so the whole suite finishes in seconds. Nothing reaches the network: the binary's
update check is skipped through `DEVCTL_UNSAFE_FORCE_VERSION`, and every endpoint the commands know is pointed at
a mock.

A failed scenario prints the description, the arguments, stdout, stderr and every request each mock received,
in order, so the sequence the command walked is on the screen.

Run one scenario:

```bash
go test ./e2e/ -run 'TestScenarios/stage-gap' -v
```

The runner declares every file of the module as an input of the test, so the test cache reruns the scenarios
after a change anywhere in devctl's sources, not only under `e2e/`; `-count=1` forces a rerun regardless.

## Layout

```
e2e/
  e2e_test.go            the runner: builds the binary, discovers and runs the scenarios
  scenario/              the format: scenario.yaml, expected.json, the wildcard match
  mock/sequence/         per-route response sequences, shared by the mocks
  mock/github/           GitHub REST API and the device-flow endpoints
  mock/circleci/         CircleCI API v2 and OAuth issuer
  mock/registry/         OCI registry (one instance public, one private)
  scenarios/<slug>/      one scenario: scenario.yaml, expected.json, fixture files
```

## Adding a scenario

1. Create `scenarios/<slug>/`. The slug names the incident (`stage-gap`, `renamed-image`); the reserved slugs
   are listed at the end.
2. Write `scenario.yaml` (the command line and what the mocks answer) and `expected.json` (the exit code and the
   JSON document). Nothing is registered anywhere: the directory is discovered.
3. Run it as above. The failure report shows the requests the command made, which is how a fixture is refined
   until it describes the incident.

## scenario.yaml

```yaml
description: a CircleCI workflow behind requires has not reported when the checks read green
args: [pr, wait, giantswarm/devctl, "42", --timeout, 30m]
env:                                   # optional, replaces the harness's value of a variable
  DEVCTL_TIME_SCALE: "0.01"
timeout: 20s                           # optional bound of the run, default 60s
keyring:                               # optional, written as JSON to $DEVCTL_KEYRING_FILE
  github:
    token: ghu_scenario
    expiresAt: "2099-01-01T00:00:00Z"
github:
  routes:
    "GET /repos/giantswarm/devctl/pulls/42":
      - body: {number: 42, state: open, draft: false, mergeable_state: clean, head: {sha: abc123}, base: {ref: main}}
    "GET /repos/giantswarm/devctl/commits/abc123/check-runs":
      - body: {total_count: 1, check_runs: [{name: go-build, status: completed, conclusion: success}]}
    "GET /repos/giantswarm/devctl/actions/runs?head_sha=abc123":
      - body: {total_count: 0, workflow_runs: []}
circleci:
  routes:
    "GET /api/v2/project/gh/giantswarm/devctl/pipeline?branch=feature":
      - body: {items: [{id: p1, number: 12, vcs: {revision: abc123}}]}
    "GET /api/v2/pipeline/p1/workflow":
      - body: {items: [{id: w1, name: build, status: running}]}
      - body: {items: [{id: w1, name: build, status: success}]}
registry:                              # the public registry, DEVCTL_REGISTRY_PUBLIC
  routes:
    "HEAD /v2/giantswarm/devctl/manifests/v1.2.3":
      - status: 404
      - status: 200
privateRegistry:                       # the private registry, DEVCTL_REGISTRY_PRIVATE
  staleLogin: true
```

| Field | Meaning |
|---|---|
| `description` | What the scenario proves, one line. Printed when it fails. |
| `args` | devctl's command line without the binary. Required. |
| `env` | Variables added to the environment; one named here replaces the harness's value. |
| `timeout` | Bound of the run (`20s`, `2m`); the scenario fails when the binary has not exited. Default 60 s. |
| `keyring` | Written as JSON to the file `DEVCTL_KEYRING_FILE` names, the record format of the keyring store. Left out, the file does not exist: the state before `devctl auth login`. |
| `github`, `circleci`, `registry`, `privateRegistry` | The mocks' scripts: `routes`, and for a registry `staleLogin`. |
| `muster` | The muster mock's script: `tools`, a tool name as muster exposes it (`x_giantswarm-repo-manager_get_info`, `core_auth_login`) to its sequence of answers (`result`: a mapping or list is the structured content, a string the text; `error`: a tool error with that text; `args`: arguments the call must carry with these values, compared as JSON -- a call without them gets a tool error naming the difference); `login`, the userinfo email; `bearer` and `refreshToken`, tokens the mock accepts besides the ones it issues (the scenario's keyring). A keyring's muster record writes `${DEVCTL_MUSTER_URL}` for its `endpoint` and `${MUSTER_ISSUER}` for its `issuer`; the harness fills in the mock's URLs. |
| `browser` | `true` makes the harness play the person at the browser: every URL devctl asks to open on stderr is fetched, following redirects, so the muster mock's authorization endpoint lands its code on devctl's loopback callback. Only loopback URLs are fetched. |

Unknown fields are errors, so a misspelt key fails the scenario instead of being ignored.

### Routes and sequences

A route key is `METHOD /path` or `METHOD /path?key=value`, the path as the real server sees it (GitHub's
`/repos/...` and `/login/...`, CircleCI's `/api/v2/...` and its OAuth paths, the registry's `/v2/...`). The
method is exact: a `HEAD` route does not answer `GET`. A key with a query matches a request that carries every
listed parameter with the listed value, whatever else the request sends (`per_page`, page tokens); a key without
a query matches any query. Of several matching routes the one with the most query parameters wins.

The value is the route's sequence of responses. The Nth request to the route gets the Nth response and the last
one repeats: a poll loop sees state advance (`in_progress`, `in_progress`, `completed`), and a check that never
reports keeps answering the same. Counting is per route, not per request, and starts at zero for every scenario.

A response has `status` (default 200), `headers` (a mock adds its own defaults for the headers a fixture leaves
out and never overrides one the fixture sets) and `body`: a mapping or list is sent as JSON, a string verbatim,
nothing as an empty body. `reset: true` answers nothing and closes the connection with a TCP reset, which the
client reads as `connection reset by peer`; net/http replays a GET whose reused connection was reset, so a
reset meant to reach devctl is scripted on a mock's first request. A request no route matches gets the mock's not-found answer (below).

## expected.json

```json
{
  "exitCode": 0,
  "json": {
    "command": "pr wait",
    "schemaVersion": 1,
    "exitCode": 0,
    "verdict": "green",
    "reason": "",
    "warnings": [],
    "startedAt": "*",
    "finishedAt": "*",
    "repository": "giantswarm/devctl",
    "number": 42,
    "headSha": "abc123",
    "checks": [
      {"name": "go-build", "source": "check_run", "status": "completed", "conclusion": "success", "url": "*", "required": true}
    ]
  }
}
```

`exitCode` is the code the binary must exit with. `stdoutContains` and `stderrContains` are substrings the
outputs must carry, for a command that speaks text. `json` is the document stdout must carry: stdout is parsed as
exactly one JSON document and compared with `json` as a whole. Objects need the same set of keys, arrays the
same length, scalars equality; the string `"*"` stands for any value (a timestamp, a URL with a port, a digest),
whatever its type. The first difference is reported by path (`at $.checks[0].status: want "completed", got
"in_progress"`). Without `json` stdout is not compared, for a scenario of a command that speaks text.

## The environment the binary gets

The binary's environment is built from scratch; nothing of the developer's shell reaches it.

| Variable | Value |
|---|---|
| `DEVCTL_GITHUB_API_URL` | the GitHub mock |
| `DEVCTL_GITHUB_OAUTH_URL` | the GitHub mock (the device-flow paths `/login/device/code`, `/login/oauth/access_token`) |
| `DEVCTL_CIRCLECI_API_URL` | the CircleCI mock plus `/api/v2` |
| `DEVCTL_CIRCLECI_OAUTH_URL` | the CircleCI mock |
| `DEVCTL_REGISTRY_PUBLIC` | `host:port` of the public registry mock |
| `DEVCTL_REGISTRY_PRIVATE` | `host:port` of the private registry mock |
| `DEVCTL_REGISTRY_INSECURE` | `1`: the mocks speak plain HTTP |
| `DEVCTL_MUSTER_URL` | the muster mock plus `/mcp` |
| `DEVCTL_KEYRING_FILE` | `keyring.json` in the scenario's home, the scenario's `keyring` when given |
| `DEVCTL_TIME_SCALE` | `0.001` |
| `DEVCTL_UNSAFE_FORCE_VERSION` | the built binary's version, which skips the update check |
| `HOME`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `TMPDIR` | directories of the scenario's own |
| `PATH` | an empty directory: no `gh`, `docker` or `git` to fall back on |

The scenario's `env` comes last and wins.

## The mocks

**GitHub** (`mock/github`) serves the REST API and the device flow from one server. Every route is the
scenario's; the mock adds what a client relies on: the rate-limit headers (`X-RateLimit-Limit: 5000`,
`X-RateLimit-Remaining: 4999`, `X-RateLimit-Reset` an hour ahead, unless the fixture sets them) and an `ETag` on
every successful response, computed from the body. A request whose `If-None-Match` equals that ETag gets
`304 Not Modified` with the rate-limit headers and no body; the sequence advances all the same, so a changed
body is sent on the next poll. An unscripted route is `404 {"message":"Not Found", ...}` as on api.github.com.
Endpoints the commands use, as route keys: `GET /repos/{o}/{r}/pulls/{n}`, `GET
/repos/{o}/{r}/commits/{sha}/check-runs`, `GET /repos/{o}/{r}/commits/{sha}/status`, `GET
/repos/{o}/{r}/actions/runs?head_sha={sha}`, `GET /repos/{o}/{r}/branches/{b}/protection`, `GET
/repos/{o}/{r}/rules/branches/{b}`, `GET /repos/{o}/{r}/rulesets`, `PUT /repos/{o}/{r}/pulls/{n}/merge`, `PUT
/repos/{o}/{r}/pulls/{n}/update-branch`, `DELETE /repos/{o}/{r}/git/refs/heads/{b}`, `GET /repos/{o}/{r}`,
`GET /repos/{o}/{r}/releases/tags/{tag}`, `GET /repos/{o}/{r}/contents/{path}`, `POST /login/device/code`,
`POST /login/oauth/access_token`, `GET /user`.

**CircleCI** (`mock/circleci`) serves the API v2 under `/api/v2` and the OAuth issuer's paths at the root, from
one server. Every route is the scenario's; an unscripted route is `404 {"message":"Not Found"}`. Endpoints, as
route keys: `GET /api/v2/project/{slug}`, `GET /api/v2/project/{slug}/pipeline?branch={b}`, `GET
/api/v2/pipeline/{id}`, `GET /api/v2/pipeline/{id}/workflow`, `GET /api/v2/workflow/{id}/job`, and for the
login the issuer's registration and token endpoints the command asks for.

**muster** (`mock/muster`) is a muster aggregator with its own OAuth 2.1 authorization server on one server. The
OAuth side is built in, not scripted: `GET /.well-known/oauth-protected-resource[/mcp]` names the server itself,
`GET /.well-known/oauth-authorization-server` its endpoints (S256 PKCE, `authorization_code` and `refresh_token`),
`POST /oauth/register` hands out one client id, `GET /oauth/authorize` redirects straight back to the client's
`redirect_uri` with a code bound to the PKCE challenge (the browser of a person who is signed in already), `POST
/oauth/token` exchanges the code (the verifier checked) or a refresh token for a fresh access and refresh token, and
`GET /oauth/userinfo` answers the scenario's `login`. `POST /mcp` speaks MCP over streamable HTTP for a bearer the
mock issued or the scenario's `bearer`, else `401` with the bearer challenge, and answers the way a muster aggregator
answers a session: `initialize` opens a session, `tools/list` lists the meta-tools only, `call_tool` answers the named
tool from the scenario's `tools` in muster's envelope (the tool's result as JSON in one text content, `isError` and the
structured content mirrored), a tool the scenario does not script is `Tool not found: <name>` as an error result, and a
server tool called directly is the JSON-RPC error `tool '<name>' not found` -- which is what devctl gets when it does not
go through `call_tool`.

**Registry** (`mock/registry`) is an OCI distribution registry, run twice: `registry` is the public one,
`privateRegistry` the private one, so a scenario can give them different states. A manifest is `HEAD` or `GET
/v2/{name}/manifests/{reference}`; a chart is a name under `charts/` (`/v2/giantswarm/charts/devctl/manifests/1.2.3`).
Manifest routes have defaults: a `200` without a body is a minimal manifest with `Content-Type` and
`Docker-Content-Digest` (set `headers: {Docker-Content-Digest: ...}` to pin a digest for `expected.json`), a
`404` without a body is the `MANIFEST_UNKNOWN` error, and a manifest no route scripts is `MANIFEST_UNKNOWN` too.
`GET /v2/` is `200 {}`. With `staleLogin: true` every request that carries an `Authorization` header is `401
UNAUTHORIZED` with a bearer challenge, the answer a registry gives a client whose stored login has expired; a
request without credentials is answered by the routes, as a public registry answers an anonymous read.

## Scenarios

`self-test-accepted` and `self-test-refused` are the harness's own: they run `repo validate` on a team file
beside the scenario, offline, and pin the runner, the wildcard match and the exit-code assertion on a passing and
on a failing command.

The incidents the commands encode, one scenario each, by these slugs:

- `pr wait`: `stage-gap`, `fork-awaiting-approval`, `conflicting-pr`, `retitled-stale-run`,
  `red-circleci-workflow`, `pr-wait-timeout`, `required-never-reported`, `github-5xx-retried`,
  `github-5xx-persistent`, `circleci-connection-reset`
- `pr merge`: `own-green-merged`, `other-human-refused`, `opt-out-refused`, `behind-strict-base`,
  `behind-update-branch`, `merge-queue`, `red-not-merged`, `merge-released`, `merge-no-release`,
  `merge-release-failed`, `merge-github-5xx-retried`
- `release wait`: `renamed-image`, `hand-written-ci`, `release-assets-only`, `failed-tag-pipeline`,
  `rerun-replaces-failed`, `stale-registry-login`, `release-wait-timeout`, `jobs-not-visible-yet`,
  `declaration-behind-repository`, `repo-owned-tag-job`, `release-wait-no-release`,
  `release-wait-github-5xx-retried`
- `auth`: `auth-missing`, `auth-expired`, `auth-refreshed`, `auth-login-muster`, `auth-login-muster-no-manager`
- `repo` (the manager's verbs over the mocked muster): `repo-auth-missing`, `repo-status`, `repo-status-json`,
  `repo-status-refreshed`, `repo-list`, `repo-info`, `repo-get`, `repo-refresh`, `repo-sweep`, `repo-watch-ready`,
  `repo-watch-failed`, `repo-adopt-dry-run`, `repo-update-set`, `repo-transfer-dry-run`, `repo-set-lifecycle-commit`,
  `repo-set-lifecycle-delete-refused`, `repo-approve`, `repo-align-dry-run`
