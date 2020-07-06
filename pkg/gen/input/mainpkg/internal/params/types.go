package params

type Params struct {
	GoModule    string
	Subcommands []Subcommand
}

type Subcommand struct {
	Alias       string
	Import      string
	ParentAlias string
}
