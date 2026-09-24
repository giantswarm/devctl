package file

// templateKeyStepSetUpGitIdentity is the template data key under which the
// workflows that commit as taylorbot receive the shared git identity step.
const templateKeyStepSetUpGitIdentity = "StepSetUpGitIdentity"

// templateKeyHeader is the template data key carrying the generated-file
// header every workflow renders first.
const templateKeyHeader = "Header"

// templateKeyReleaseCandidateByDefault is the template data key saying the
// auto-release workflow cuts a release candidate on every push, leaving the
// stable release to a manual run.
const templateKeyReleaseCandidateByDefault = "ReleaseCandidateByDefault"

// The workflow templates render GitHub Actions ${{ }} expressions verbatim, so
// they use four braces as their own action delimiters.
const (
	templateDelimLeft  = "{{{{"
	templateDelimRight = "}}}}"
)
