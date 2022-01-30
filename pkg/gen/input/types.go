package input

type Input struct {
	Delete bool
	// Path is the absolute path of the file to be generated from this
	// Input.
	Path string
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
