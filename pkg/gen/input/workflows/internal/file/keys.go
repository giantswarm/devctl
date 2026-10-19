package file

// templateKeyStepSetUpGitIdentity is the template data key under which the
// workflows that commit as taylorbot receive the shared git identity step.
const templateKeyStepSetUpGitIdentity = "StepSetUpGitIdentity"

// templateKeyHeader is the template data key carrying the generated-file
// header every workflow renders first.
const templateKeyHeader = "Header"

// templateKeyFork is the template data key saying the repository is a fork
// line (flavour fork), whose auto-release counts from the line's highest
// stable tag.
const templateKeyFork = "Fork"

// templateKeyMaintenanceBranches is the template data key saying a fork
// line's maintenance branches (release-X.Y) cut releases of their series.
const templateKeyMaintenanceBranches = "MaintenanceBranches"

// templateKeyReleaseBranch is the template data key carrying the branch whose
// pushes cut releases.
const templateKeyReleaseBranch = "ReleaseBranch"

// templateKeyAdvisoryJobs is the template data key carrying the CircleCI
// jobs whose failure does not fail a release, space-separated: the
// auto-release promote step warns on them as devctl's release commands do.
const templateKeyAdvisoryJobs = "AdvisoryJobs"

// The workflow templates render GitHub Actions ${{ }} expressions verbatim, so
// they use four braces as their own action delimiters.
const (
	templateDelimLeft  = "{{{{"
	templateDelimRight = "}}}}"
)
