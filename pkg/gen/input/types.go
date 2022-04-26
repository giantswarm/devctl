package input

import (
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
}

type InputTemplateDelims struct {
	Left  string
	Right string
}
