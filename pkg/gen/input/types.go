package input

import (
	"text/template"
)

type Input struct {
	// Path is the absolute path of the file to be generated from this
	// Input.
	Path string
	// TemplateBody is the Go text template from which the file is
	// generated.
	// PostProcessGoFmt indicates whether to run `go fmt` on the generated
	// output.
	PostProcessGoFmt bool
	// TemplateBody is the body of the template to be rendered.
	TemplateBody string
	// TemplateData defines data for the template defined in TemplateBody.
	TemplateData interface{}
	// TemplateDelims are used to call
	// https://golang.org/pkg/text/template/#Template.Delims if set.
	TemplateDelims InputTemplateDelims
	// TemplateFuncs allow to specify custom functions for the template.
	// See https://golang.org/pkg/text/template/#FuncMap.
	TemplateFuncs template.FuncMap
}

type InputTemplateDelims struct {
	Left  string
	Right string
}
