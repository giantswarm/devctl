package params

type Params struct {
	// CurrentFlavour is the desired type of Makefile that should be generated.
	CurrentFlavour int

	FlavourApp      int
	FlavourCLI      int
	FlavourLibrary  int
	FlavourOperator int
}
