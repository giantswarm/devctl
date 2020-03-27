package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string
	// ObjectGroup of the object reconciled by the generated resource.
	ObjectGroup string
	// ObjectKind of the object reconciled by the generated resource.
	ObjectKind string
	// ObjectVersion of the object reconciled by the generated resource.
	ObjectVersion string
}
