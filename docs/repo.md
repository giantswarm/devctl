# Repository set-up with the `repo` commands

Giant Swarm repositories are declared, not clicked together: one entry in the owning team's file
`repositories/<team>.yaml` of [giantswarm/github](https://github.com/giantswarm/github) is the
desired state, and the reconciler creates and keeps the repository as declared. The `repo` commands
are the laptop's client of that engine -- the same validation, the same dry run, the same pull request
the Repositories page and giantswarm-repo-manager produce.

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

`gen.flavours` and `gen.language` are mandatory for a repository the command creates;
`gen.ci.generate` is written as the CircleCI generator decides: `true` when it has a job for the
declaration (a Go or Node build, a chart from the `app` flavour), `false` when the pipeline would be empty. The help text takes the enums from the schema devctl ships
(`pkg/reposetup/schema/repositories.schema.json`) and the flavours and languages from devctl's own
lists, so it never lags a copy in this document; the live schema is
[`.github/repositories.schema.json`](https://github.com/giantswarm/github/blob/main/.github/repositories.schema.json)
in giantswarm/github.

The template is derived, not chosen: `language: go` gives `giantswarm/template`; `language: generic`
with the `app` flavour gives `giantswarm/template-app`; a customer repository, `python`,
`kyverno-policy` and every other combination give the minimal scaffold.

### Token

`$GITHUB_TOKEN` (`--github-token-envvar` names another variable) or, when unset, the login of your
`gh` CLI (`gh auth token`). The repository, its scaffold commit and the pull request are yours: all
three are written with that identity. The token creates the repository and pushes its scaffold
(`repo`, and `workflow` for the scaffold's GitHub Actions workflows), reads the organisation's teams
and your role in it (`read:org`) and writes `giantswarm/github` for the pull request.

## `devctl repo status`

Prints a repository's set-up state -- every set-up step with its verdict and whether the repository
is set up as declared:

```nohighlight
devctl repo status my-service
devctl repo status giantswarm/my-service --team bumblebee --output json
```

With a muster endpoint (`--muster-endpoint`, or `$MUSTER_ENDPOINT`; the bearer token in
`$MUSTER_TOKEN`) the state comes from giantswarm-repo-manager's inventory, as you. When the manager is
not configured or cannot be reached, the engine's checks run locally in read mode with your GitHub
token (and your CircleCI token from `$CIRCLECI_TOKEN` for the CircleCI and release steps; without one
those steps are skipped). Both paths print the same verdicts the Repositories page shows:

| Verdict | Meaning |
|---|---|
| `ok` | no drift |
| `drift` | drift found; the lines `would: …` are the changes a repair would make |
| `reported` | nothing the engine repairs, but findings with their fix for a person |
| `skipped` | the step does not apply (repository missing or empty, archived, no client for the system) |
| `failed` | the step could not run to its end |

The repository must be declared in a team file; an undeclared repository is reported as such with
`devctl repo create` as the fix. `--team` names the file to read instead of searching them all.
Nothing is changed by `repo status`.

## `devctl repo validate`

Validates entries of a local team file and prints the dry run as JSON -- what the validation check on
`giantswarm/github` runs. `--mode create|existing` says what the entries are validated for:

| Mode | Applies | Default when |
|---|---|---|
| `create` | the schema, the creation rules (`gen.flavours` and `gen.language` set, a template for them, a job for generated CI, the chart-name convention) and a free name on GitHub; the guard notices say what review the change gets | `--entry` names the entries being added |
| `existing` | the schema alone -- an entry the schema accepts is valid however it predates the creation rules; the name check's verdict is reported and never refuses, a missing repository being the reconciler's finding | no `--entry`: the whole file is on main already |

`repo status` and `repo reconcile` validate a declared repository's entry in existing mode (`repo
reconcile --added` in create mode: the repository is created). See `devctl repo validate --help`.

## `devctl repo reconcile`

Runs the set-up steps as the person (`--dry-run` checks only; `--output json` prints the result the
reconciler stores). An entry the validator refuses is a result too: one step, `entry`, verdict
`reported`, one finding per problem (`gen-circleci-refused` for `gen.ci.generate`, `entry-refused`
otherwise) with the field to fix, exit 0 -- the declaration is at fault, not the run. A flag or token
error exits 2. `--enforce-admins` (default true) is the one baseline knob: whether the branch protection
binds administrators too; the default stands until giantswarm/giantswarm#36733 decides the baseline.

The `catalog` step dispatches the catalog and mapping workflows of the catalog repository, which needs an
Actions permission there. `--dispatch-token-envvar` names a second token for those two calls (listing the
workflow's runs, dispatching it) when the GitHub token's identity has none -- the reconciler passes its
workflow run's own token, the App holding no Actions permission. Every read stays with the GitHub token,
the repository lookup included: a private repository the dispatch token cannot see is looked up, checked
against the catalog and dispatched all the same. Without the flag the GitHub token dispatches.
