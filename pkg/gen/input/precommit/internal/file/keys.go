package file

// templateKeyHeader is the template data key carrying the generated-file
// header every input renders first.
const templateKeyHeader = "Header"

// The pre-commit workflow template renders GitHub Actions ${{ }} expressions
// verbatim, so it uses square brackets as its own action delimiters.
const (
	templateDelimLeft  = "[["
	templateDelimRight = "]]"
)
