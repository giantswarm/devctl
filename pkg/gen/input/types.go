package input

type Input struct {
	// Path is the absolute path of the file to be generated from this
	// Input.
	Path string
	// Scaffolding determines whether the generated file is a scaffolding
	// file. Scaffolding files are supposed to be edited by the user and
	// never regenerated.
	Scaffolding bool
	// TemplateBody is the Go text template from which the file is
	// generated.
	TemplateBody string
	// TemplateData defines data for the template defined in TemplateBody.
	TemplateData interface{}
}
