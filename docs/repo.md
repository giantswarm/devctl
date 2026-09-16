# Repository set-up with the `repo` commands

Giant Swarm repositories are declared, not clicked together: one entry in the owning team's file
`repositories/<team>.yaml` of [giantswarm/github](https://github.com/giantswarm/github) is the
desired state, and the reconciler creates and keeps the repository as declared. The `repo` commands
are the laptop's client of that engine -- the same validation, the same dry run, the same pull request
the Repositories page and giantswarm-repo-manager produce.

## `devctl repo create`

Declares a new repository and opens the team-file pull request as you:

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
   the notices -- and
5. opens the pull request on a `repo-create/<name>` branch with the conventional-commit title
   `feat(<team>): declare <name>` and a body naming the declaration.

A refusal (a taken name, a wrong flavour, a schema violation) ends the command with a non-zero exit
before any pull request exists; the problems name the fields. `--dry-run` stops after the dry run,
`--output json` prints the dry run (and the pull request URL) as JSON.

The command never creates a repository and never touches GitHub settings: a repository without its
declaration is the drift the reconciler reports. After the merge the reconciler creates the
repository, pushes the scaffold, applies the set-up and runs the first release.

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
| `--description` | `description` | free text, set on GitHub by the reconciler |
| `--visibility` | `visibility` | the schema's enum -- `devctl repo create --help` lists it |

`gen.flavours` and `gen.language` are mandatory for a repository the reconciler creates;
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
`gh` CLI (`gh auth token`). The pull request is yours: it is opened with that identity.

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
