// Package reposetup is the front half of the repository set-up engine: it
// reads a team file of giantswarm/github, validates its entries against the
// repositories schema — and, for the entries about to create a repository,
// the creation rules — derives the template each repository is scaffolded
// from and returns the dry-run value the clients render — the team-file
// entry as it would be written, the implied template, the verdict of the
// name check and the guard notices.
//
// The package is imported by `devctl repo validate` (and `repo create`), by
// the validation workflow of giantswarm/github and by giantswarm-repo-manager's
// validate_repository tool, so validation and rendering exist once. It keeps
// no CLI state: every input is a value of [Request], every output a value of
// [Result], and GitHub is reached only through the small [FileGetter] and
// [RepositoryGetter] interfaces, which *githubclient.Client satisfies.
//
// # Declarations
//
// A [TeamFile] is one repositories/<team>.yaml. Its team is the file's base
// name (repositories/team-bumblebee.yaml → team-bumblebee), the GitHub team
// slug CODEOWNERS names for the file; an entry needs no team field. Each
// [Declaration] is one entry, kept as the YAML node it was read from so the
// rendered entry keeps the author's key order and comments.
//
// # Rules
//
// [Request.Mode] says what the entries named in [Request.Names] (all entries
// when nil) are validated for. In [ModeExisting] they declare repositories
// that exist and the schema alone decides: an entry the schema accepts is
// valid, however it predates the creation rules, and it is rendered as
// declared — no default is written, an entry without gen.ci keeps the
// repository's own CircleCI configuration; the name check's verdict is
// reported and never refuses, a missing repository being the reconciler's
// finding. In [ModeCreate] (the default) they are repositories the
// reconciler creates and must satisfy, on top of the schema:
//
//   - gen.flavours and gen.language are set; gen.ci.generate defaults to
//     true and is written into the rendered entry;
//   - the name is lowercase and free on GitHub — an existing repository or a
//     redirect from a renamed one counts as taken;
//   - the chart-name convention holds where a chart exists (the app or
//     cluster-app flavour): the repository is named after its chart, without
//     an -app suffix, and gen.ci.chartName, when set, equals the name;
//   - a template exists for the language: language node is refused until
//     giantswarm/template-node ships.
//
// Every refusal is a [Problem] naming the field in dotted form
// (gen.ci.chartName, gen.flavours[1]).
//
// # Templates
//
// [DeriveTemplate] maps a declaration to its template without a template
// field: language go → giantswarm/template; language generic with the app
// flavour → giantswarm/template-app; the customer flavour or component type,
// the languages python and kyverno-policy, and generic repositories without
// a chart → the minimal scaffold (README, LICENSE, DCO, SECURITY.md,
// CODEOWNERS, .gitignore plus the generated files).
//
// # Rendering
//
// [Renderer.Render] renders the scaffold of an accepted [Entry] into a
// directory: the template's tree (the tarball of its main branch, or a
// [TemplateSource] of the caller's) with its placeholders replaced -- what
// `devctl replace` does by hand -- CODEOWNERS for the team, the chart's team
// annotation and default icon, and the files the generators write for the
// declared flavours and language: Makefile, workflows including
// auto-release, LLM rules, pre-commit, and CircleCI and Renovate when
// gen.ci.generate is on. The generators run through the same `devctl gen`
// commands align-files runs, in align-files' order and with its flags, so
// the generated files are byte-identical to what the first align run would
// write and that run changes nothing. [Scaffold.Commands] lists the command
// lines. A chart repository whose pipeline runs app-test-suite also gets
// the chart tests that job runs, beside the generated tests/ats/pyproject.toml
// and where the template carries none: .ats/main.yaml, skipping the
// functional and the upgrade scenario a new repository cannot run, and
// tests/ats/test_smoke.py, one smoke test that the job's kind cluster is
// reachable; both are the repository's own from then on, never written by
// an align run. The chart-only template offers [Option]s -- the vendir sync and
// patch-script scaffolding `devctl app bootstrap` used to write by flag --
// listed on [Entry.Options] by the dry run and chosen through
// [RenderRequest.Options].
//
// # Guards
//
// Two notices bound the machine approval of a creation-only pull request: an
// author outside the owning team (and outside [FallbackTeam], the CODEOWNERS
// fallback) keeps the team's review, and more than [MaxMachineApprovedEntries]
// added entries get a person. Author and membership are inputs; the package
// resolves neither.
//
// # Schema
//
// [FetchSchema] reads the schema from giantswarm/github main so a run
// follows the schema as it is; [EmbeddedSchema] is the copy shipped with
// this devctl version, for tests and as the fallback when GitHub cannot be
// reached. [Result.Schema] says which one a result was validated against.
package reposetup
