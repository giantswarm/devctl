package file

// templateKeyStepSetUpGitIdentity is the template data key under which the
// workflows that commit as taylorbot receive the shared git identity step.
const templateKeyStepSetUpGitIdentity = "StepSetUpGitIdentity"

// templateKeyHeader is the template data key carrying the generated-file
// header every workflow renders first.
const templateKeyHeader = "Header"

// The workflow templates render GitHub Actions ${{ }} expressions verbatim, so
// they use four braces as their own action delimiters.
const (
	templateDelimLeft  = "{{{{"
	templateDelimRight = "}}}}"
)
