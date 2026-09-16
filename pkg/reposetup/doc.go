// Package reposetup is the front half of the repository set-up engine: it
// reads a team file of giantswarm/github, validates the entries about to
// create a repository against the repositories schema and the creation
// rules, derives the template each repository is scaffolded from and
// returns the dry-run value the clients render — the team-file entry as it
// would be written, the implied template, the verdict of the name check and
// the guard notices.
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
// Every entry named in [Request.Names] (all entries when nil) is treated as a
// repository the reconciler creates and must satisfy, on top of the schema:
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
// CODEOWNERS, .gitignore plus the generated files). Rendering the scaffold is
// the engine's back half.
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
