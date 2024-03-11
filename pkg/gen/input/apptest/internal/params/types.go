package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string

	AppName  string
	RepoName string
	Catalog  string
}
