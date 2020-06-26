package params

type Params struct {
	// Name is the name of CLI binary name.
	Name string

	Subcommands []Subcommand
}

type Subcommand struct {
	Alias       string
	Import      string
	ParentAlias string
}
