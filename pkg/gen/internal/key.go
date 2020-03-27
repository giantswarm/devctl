package internal

const (
	// RegenerableFilePrefix defines a prefix for files that can be
	// regenerated in a subsequent generator execution. Otherwise the file
	// is considered a scaffolding file which is generated once and
	// supposed to be edited by the user.
	//
	// NOTE: It is important to design scaffolding files in a way so they
	// stay compatible with regenerated files as they are not updated in
	// subsequent generator executions.
	RegenerableFilePrefix = "zz_generated."
)
