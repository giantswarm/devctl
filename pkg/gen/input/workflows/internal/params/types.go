package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string

	// CurrentFlavour is the desired type of workflow that should be generated.
	CurrentFlavour int

	FlavourApp      int
	FlavourCLI      int
	FlavourLibrary  int
	FlavourOperator int
}
