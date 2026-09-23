# Repository set-up with the `repo` commands

Giant Swarm repositories are declared, not clicked together: one entry in the owning team's file
`repositories/<team>.yaml` of [giantswarm/github](https://github.com/giantswarm/github) is the
desired state, and the reconciler creates and keeps the repository as declared. The `repo` commands
are the laptop's client of that engine and of giantswarm-repo-manager -- the same validation, the same
dry run, the same pull request the Repositories page and the Repo Manager agent produce. `create`,
`validate` and `reconcile` run the engine locally with your own tokens; the other verbs call the
manager through muster as you.

## `devctl repo create`

Creates a repository as you and declares it: the repository, its scaffold, then the team-file pull
request.

```nohighlight
devctl repo create --team bumblebee --name my-service \
  --component-type service --flavour app --language go \
  --description "What it does" --visibility public
```

The command

1. reads the team's file from `giantswarm/github` at `main` (an unknown team ends here),
2. renders the entry and places it alphabetically among the team's entries -- the rest of the file is
   kept byte for byte,
3. validates the changed file through the engine: the repositories schema (read from
   `giantswarm/github` `main`, the embedded copy as the fallback), the creation rules and the name on
   GitHub (an existing repository or the redirect of a renamed one is taken),
4. prints the dry run -- the rendered entry, the template it derives, the name check, the verdict and
   the notices,
5. reads your role in the organisation and refuses anyone who is not an owner (below), before
   anything is written,
6. creates the repository with your GitHub login -- description and visibility from the declaration;
   you are its admin, as the creator of an organisation repository is,
7. pushes the rendered scaffold as the one commit on `main` (`feat: initial scaffold of <name> from
   <template>`); the scaffold's auto-release workflow tags `v0.1.0` from it and CircleCI builds the
   tag once the reconciler has followed the project, and
8. opens the pull request on a `repo-create/<name>` branch with the conventional-commit title
   `feat(<team>): declare <name>` and a body naming the repository, its scaffold commit and the
   declaration -- validated in existing mode now, since the repository exists.

The output names the repository, the scaffold commit and the pull request; `--output json` prints the
dry run, the creation (`create`: the two steps, `url`, `created`, `scaffoldCommit`) and the pull
request URL as one document. `--dry-run` prints the dry run and the plan of the creation (the two
steps as `drift`, what each would do) and writes nothing.

The create and scaffold steps are the engine's own (`reconcile.Runner.Create`, the same steps the
reconciler runs), so the repository the command creates and the one the reconciler would have created
are the same. Everything after the scaffold -- settings, team permissions, branch protection and the
required checks, CircleCI, webhooks, CODEOWNERS, the catalog, the first-release check -- the
reconciler applies from the merged entry; it repairs, and never creates.

### Only an organisation owner creates

The organisation does not let members create repositories: GitHub answers a member's creation with
403. The command reads your role (`GET /user/memberships/orgs/giantswarm`) before the first write and
refuses anyone but an owner with

```nohighlight
only an organization owner may create a repository in giantswarm — ask an owner, or create it from
the Dev Portal (which also creates it as you and needs the same role)
```

The engine gives the same answer to a 403 on the creation itself. The Dev Portal creates the
repository as the signed-in person too, so the role is needed there as well.

### A refusal, an interrupted run

A refusal of the declaration (a taken name, a wrong flavour, a schema violation) ends the command with
a non-zero exit before anything exists; the problems name the fields.

A run interrupted after the creation resumes on the next call: a repository of the declared name that
you administer -- the name itself, not a redirect -- is yours to continue. The dry run then validates
the entry in existing mode, the create step finds the repository (`exists`), the scaffold step pushes
the scaffold when `main` has none (`present` otherwise), and the pull request is opened when none is
open for the `repo-create/<name>` branch (the open one is reported otherwise). A repository of that
name that someone else administers stays a refusal. A step that fails (the scaffold's template cannot
be downloaded, the CircleCI generator refuses the declaration) ends the command with the step's
message; the rerun resumes where it stopped.

### What review the pull request gets

The validation check on `giantswarm/github` classifies every team-file pull request. A
*creation-only* change -- entries added, every one valid -- is approved by the machine and merges on
its own when the author is a member of the owning team or of `team-planeteers`, and when at most
three entries are added. The notices in the dry run say beforehand when this does not hold
(`team-review`: the team's review is required; `batch-review`: a person reviews). Membership is read
from GitHub as you, which needs a token that can read the organisation's teams (`read:org`); without
it the notice is not given and the command says so.

### The declaration's fields

| Flag | Field | Valid values |
|---|---|---|
| `--team` | the file the entry lives in | a team with a file under `repositories/`; `bumblebee` and `team-bumblebee` both work |
| `--name` | `name` | the slug of `https://github.com/giantswarm/<name>`; chart repositories (`app` or `cluster-app` flavour) are lowercase letters, digits and hyphens without an `-app` suffix |
| `--component-type` | `componentType` | the schema's enum -- `devctl repo create --help` lists it |
| `--flavour` (repeatable) | `gen.flavours` | devctl's flavours -- `devctl repo create --help` lists them; see [flavours](flavours.md) |
| `--language` | `gen.language` | devctl's languages -- `devctl repo create --help` lists them; `node` is refused until its template exists |
| `--description` | `description` | free text, set on the repository at its creation |
| `--visibility` | `visibility` | the schema's enum -- `devctl repo create --help` lists it |
| -- | `align` | always `true`: the repository is opted in to alignment by its creation, so the reconciler changes it to its declared set-up on every trigger. An existing repository opts in when its team adds the field to its entry; without it every run is a check that changes nothing |

`gen.flavours` and `gen.language` are mandatory for a repository the command creates;
`gen.ci.generate` is written as the CircleCI generator decides: `true` when it has a job for the
declaration (a Go or Node build, a chart from the `app` flavour), `false` when the pipeline would be empty. The help text takes the enums from the schema devctl ships
(`pkg/reposetup/schema/repositories.schema.json`) and the flavours and languages from devctl's own
lists, so it never lags a copy in this document; the live schema is
[`.github/repositories.schema.json`](https://github.com/giantswarm/github/blob/main/.github/repositories.schema.json)
in giantswarm/github.

The template is derived, not chosen: `language: go` gives `giantswarm/template`; `language: generic`
with the `app` flavour gives `giantswarm/template-app`; a customer repository, `python`,
`kyverno-policy` and every other combination give the minimal scaffold. A declaration whose flavours
produce a chart (`app`, `cluster-app`) and whose template has no chart of its own -- the Go service
with the `app` flavour -- gets the chart of `giantswarm/template-app` at `helm/<name>` beside the
template, with `.abs/main.yaml` pointing at it, the name substituted and the team annotation set, so
the first release's chart job builds; the dry run names it on the `chart:` line.

### Token

`$GITHUB_TOKEN` (`--github-token-envvar` names another variable) or, when unset, the login of your
`gh` CLI (`gh auth token`). The repository, its scaffold commit and the pull request are yours: all
three are written with that identity. The token creates the repository and pushes its scaffold
(`repo`, and `workflow` for the scaffold's GitHub Actions workflows), reads the organisation's teams
and your role in it (`read:org`) and writes `giantswarm/github` for the pull request.

## The manager's verbs: one subcommand per tool of giantswarm-repo-manager

giantswarm-repo-manager keeps the inventory of the org's repositories and lands every team-file change
as the person, with the team's ask in Slack and the bookkeeping of the reconciler's runs; the
Repositories page and the Repo Manager agent are its clients, and so are these commands. Each one is one
tool of the manager, called through your muster endpoint with the muster token of the keychain
(`devctl auth login --muster-only`, see [auth](auth.md)) -- through muster's `call_tool` meta-tool, the
way muster exposes every server's tool to a session -- and printed as text, or with `-o json` as the
manager answered. Every write takes `--dry-run` (the rendered change, the pull request and the ask as
they would be; nothing written) and otherwise lands as a team-file pull request opened as you: the
manager knows no other write mode, because a repository changed on GitHub without its declaration is the
drift the reconciler reports. Without a usable muster token every one of them exits naming the login,
and nothing runs locally in its place.

| Command | Tool | What it does |
|---|---|---|
| `repo info` | `get_info` | how the manager sees the call: the caller, the identities, the inventory, the engine, the write modes |
| `repo list [--scope mine\|team\|unassigned\|all] [filters]` | `list_repositories` | the inventory, one row per repository with team, lifecycle, Renovate, set-up state, last person commit and findings; the page's filters (`--team`, `--search`, `--renovate`, `--visibility`, `--fork`, `--archived`, `--lifecycle`, `--inactive-days`, `--finding`, `--orb`, `--arm64`, `--china-push`, `--signing`, `--limit`) |
| `repo get <repo>` | `get_repository` | the whole record: declaration, GitHub, CircleCI, the CI configuration's facts, Renovate, catalog and mapping, the set-up steps and runs, the findings |
| `repo refresh <repo>` | `refresh_repository` | the record rebuilt now (the engine's checks in read mode) and printed like `get`; the cache only, nothing on GitHub |
| `repo status <repo>` | `get_repository` | the set-up state alone, as before: the steps and their verdicts, the opt-in to alignment and the declared branch and flavours from the entry, the last run and the run awaited |
| `repo sweep` | `sweep_inventory` | the full sweep started now, for a member of the manager's owning teams |
| `repo watch <repo> --pull-request N` | `watch_repository` | a new repository followed to readiness: created, scaffolded, declared, merged, set up, released; the phases printed as they complete, `--timeout` (15 m) for the whole |
| `repo adopt <repo> --team T [entry flags]` | `adopt_repository` | an existing, undeclared repository declared in the team's file; `--component-type`, `--description`, `--visibility`, `--language`, `--flavour`, `--ci-generate`, `--align`, `--lifecycle deprecated\|archived` |
| `repo update <repo> --set path=value \| --unset path \| --entry-file f` | `update_repository` | the entry changed: `--set` edits the inventory's entry field by field (`gen.ci.generate=false`, `align=true`, `gen.flavours=[app, k8sapi]`; the value is YAML), `--entry-file` passes the whole entry |
| `repo transfer <repo> --to-team T` | `transfer_repository` | the entry moved to another team's file; the receiving team approves, the giving team is told |
| `repo set-lifecycle <repo> deprecated\|archived\|deleted` | `set_lifecycle` | the lifecycle set; `deleted` needs `--confirm <repo>` |
| `repo approve <pull-request>` | `approve_change` | the approving review as you after the team check, and the merge (or the auto-merge); what the Slack ask's button does |
| `repo align <repo> [--team T]` | `align_repository` | Align now in the mode the entry decides: `align` (the reconciler dispatched), `opt-in` (the pull request that sets `align: true`), `check` (an undeclared repository checked from the team); `--dry-run` shows the plan first |

`--reason` on the writes goes into the pull request body and the ask. The dry run of a write prints the
entry before and after, the pull request (title, branch, files, who opens it) and the ask and notice with
the channel they reach or why they cannot; a commit prints the pull request's URL, whether the ask was
delivered, and the run the record now expects. `create`, `validate` and `reconcile` stay what they are:
the engine run locally with your own tokens.

```nohighlight
devctl repo list --scope unassigned --inactive-days 365
devctl repo get my-service
devctl repo watch new-service --pull-request 4711
devctl repo adopt old-tool --team team-bumblebee --component-type tool --language go --dry-run
devctl repo update my-service --set gen.ci.generate=false --reason "no pipeline"
devctl repo set-lifecycle old-tool archived --reason "replaced by new-tool"
devctl repo approve 6179
devctl repo align my-service --dry-run
```

## `devctl repo status`

Prints a repository's set-up state from giantswarm-repo-manager's inventory -- every set-up step with
its verdict and whether the repository is set up as declared:

```nohighlight
devctl repo status my-service
devctl repo status giantswarm/my-service --output json
```

The verdicts are the ones the Repositories page shows:

| Verdict | Meaning |
|---|---|
| `ok` | no drift |
| `drift` | drift found; the lines `would: …` are the changes a repair would make |
| `reported` | nothing the engine repairs, but findings with their fix for a person |
| `skipped` | the step does not apply (repository missing or empty, archived or deleted, no client for the system) |
| `failed` | the step could not run to its end |

Under the steps: the verdict line, the last reconciler run with what it was for (created, added,
transferred, archived, deprecated, changed, dispatched, nightly), the run the record expects after a
pull request or an Align now, a run that never reported, and the inventory's own findings. Above them,
from the entry as the team file carries it: whether the repository is opted in to alignment (`align:
true`; without it the reconciler checks the repository and changes nothing) and the declared default
branch and flavours. An undeclared repository is refused with `devctl repo adopt` as the fix; a
repository the inventory has not checked yet names `devctl repo refresh`. `-o json` prints the record as
the manager answered. Nothing is changed by `repo status`; the local check with your own tokens is
`devctl repo reconcile --dry-run`.

## `devctl repo validate`

Validates entries of a local team file and prints the dry run as JSON -- what the validation check on
`giantswarm/github` runs. `--mode create|existing` says what the entries are validated for:

| Mode | Applies | Default when |
|---|---|---|
| `create` | the schema, the creation rules (`gen.flavours` and `gen.language` set, a template for them, a job for generated CI, the chart-name convention) and a free name on GitHub; the guard notices say what review the change gets | `--entry` names the entries being added |
| `existing` | the schema alone -- an entry the schema accepts is valid however it predates the creation rules, and it is rendered as declared: no default is written, `gen.ci` left out keeps the repository's own CircleCI configuration; the name check's verdict is reported and never refuses, a missing repository being the reconciler's finding | no `--entry`: the whole file is on main already |

`repo status` and `repo reconcile` validate a declared repository's entry in existing mode (`repo
reconcile --added` in create mode: the repository is created). See `devctl repo validate --help`.

## `devctl repo reconcile`

Runs the set-up steps as the person (`--dry-run` checks only; `--output json` prints the result the
reconciler stores). An entry the validator refuses is a result too: one step, `entry`, verdict
`reported`, one finding per problem (`gen-circleci-refused` for `gen.ci.generate`, `entry-refused`
otherwise) with the field to fix, `converged: false` (nothing was checked; not drift either, the fix is
in the declaration), exit 0 -- the declaration is at fault, not the run. A flag or token error exits 2.

### The settings step

The repository carries the company baseline's settings: issues on, wiki and projects off; squash merges
alone, the squash commit named after the pull request's title (`PR_TITLE`, what the title check validated
and auto-release reads); the head branch updatable and deleted on merge, auto-merge on; the declared
default branch (the baseline's `main` when the entry declares none); `write` as the workflows' default
`GITHUB_TOKEN` permission. Only the fields that differ are sent. A fork line (flavour `fork`) keeps rebase
merges on and its merge commits as they are: its carried patches land one upstream-ready commit each and a
re-pin merges upstream's history; the rest of the baseline applies to it as everywhere. A customer
repository (flavour `customer`) keeps its own default branch. The six merge settings and the squash title
reach `GET /repos/{owner}/{repo}` for an admin identity only; a read identity reads them through GraphQL
and, when that fails too, reports them as the finding `unchecked` rather than as drift.

### The protection step

The default branch carries the company baseline's protection: one required review; the required status
checks on the reported-only rule (a context is required once it has reported on the default branch or a
recently merged pull request, a required context nothing reports is removed, the entry's `requiredChecks`
are required whatever reported and never removed), a branch need not be up to date to merge; no deletion
and no force push. A customer repository (flavour `customer`) keeps its own protection.

`--devctl-app-id`, the devctl GitHub App's numeric id (the App's settings page; not the client id), is the
switch between the two forms of that protection. Without it the step writes classic branch protection, as
it always has (administrators bound too, `enforce_admins`), and reports the missing id as the advisory
finding `rulesets-not-enabled`, which does not keep the repository from converging. With it the protection
is one repository ruleset, `devctl: default branch`, active on
`~DEFAULT_BRANCH` so a rename or a fork line's declared branch needs no change: the same rules, a GitHub
Actions gate pinned to the GitHub Actions App so no other integration satisfies its context, and three bypass
actors in `pull_request` mode: the devctl App, the repository admins (GitHub's repository role Admin, id 5) and
the repository's owning team -- the organization's team
of the team file's slug (`repositories/team-<slug>.yaml` names `team-<slug>`), its id read once per run
(the token reads it with the organization's members read permission). The team is an actor because GitHub
evaluates a request made with the App's user access token as the person, not as the App: the App's own
bypass covers the App acting as itself, which devctl never does; a team member's own green pull request
merges through their token by the team's bypass, an admin's in every aligned repository by the admins', as
classic protection without `enforce_admins` let them. A bypass merges a pull request the required review
would hold; direct pushes stay forbidden and every bypass is in the repository's audit log. `agentMerge:
false` leaves the ruleset without bypass actors so nothing merges past the review. A secret team cannot be
a bypass actor (GitHub refuses it): the ruleset is written with the App and the admins, and the finding
`team-bypass-refused` names the team and the fix, its privacy set to closed. Classic protection gives way
to the ruleset in the same run: its required checks are carried over, the ruleset is written, then the
classic protection is removed; the dry run plans both. A ruleset the engine did not create is left alone
and reported (advisory: it does not keep the repository from converging).

The id is the switch, not the devctl release: the reconciler's wiring passes it, so the day the reconciler
gets the id is the day its opted-in repositories move to rulesets, and a run without it -- a laptop, an
older wiring -- changes no ruleset and removes no classic protection.

The `catalog` step dispatches the catalog and mapping workflows of the catalog repository, which needs an
Actions permission there. `--dispatch-token-envvar` names a second token for those two calls (listing the
workflow's runs, dispatching it) when the GitHub token's identity has none -- the reconciler passes its
workflow run's own token, the App holding no Actions permission. Every read stays with the GitHub token,
the repository lookup included: a private repository the dispatch token cannot see is looked up, checked
against the catalog and dispatched all the same. Without the flag the GitHub token dispatches. A chart
reference that is a template's placeholder (`{APP-NAME}`) is no chart to map: the mapping's generator drops it,
and so does the step. The scaffold step's chart check does not read the chart of a `componentType: template`
entry either -- it lives under a placeholder directory and the template's own pipeline builds a rendered copy
(`docs/gen.md`).
