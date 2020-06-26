package params

type Params struct {
	// Name is the name of CLI binary name.
	Name string

	RootCommand ParamsCommandTree
}

type ParamsCommandTree struct {
	Name        string
	Subcommands []ParamsCommandTree
}

type Subcommand struct {
	Alias       string
	Import      string
	ParentAlias string
}
