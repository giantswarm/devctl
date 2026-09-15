package input

import (
	"context"
	"io/fs"
)

type Input struct {
	// If delete is true, the file will be deleted if it exists. Allows
	// for files to be moved/renamed.
	Delete bool
	// Path is the absolute path of the file to be generated from this
	// Input.
	Path string
	// Permissions to generate the file with.
	Permissions fs.FileMode
	// TemplateBody is the Go text template from which the file is
	// generated.
	TemplateBody string
	// TemplateData defines data for the template defined in TemplateBody.
	TemplateData interface{}
	// TemplateDelims are used to call
	// https://golang.org/pkg/text/template/#Template.Delims if set.
	TemplateDelims InputTemplateDelims
	// SkipRegenCheck if set skips over the `isRegenerable` check when creating files
	SkipRegenCheck bool
	// Generate, when set, produces the file's content directly instead of
	// executing TemplateBody. Used for content that isn't template-shaped Go
	// text, e.g. computed by calling another library in-process (such as
	// helm/<chart>/values.schema.json, see the precommit input package).
	// Generate and TemplateBody are mutually exclusive: an Input that sets
	// both is rejected.
	Generate func(ctx context.Context) ([]byte, error)
}

type InputTemplateDelims struct {
	Left  string
	Right string
}
