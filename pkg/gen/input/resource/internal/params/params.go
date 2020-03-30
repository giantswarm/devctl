package params

type Params struct {
	// Dir is the name of the directory where the files of the resource
	// should be generated.
	Dir string
	// ObjectFullType is a fully qualified type of the object watched by
	// the controller. It must be a pointer. E.g.
	// "*github.com/user/repo/pkg.MyType".
	ObjectFullType string
	// ObjectImportAlias of the object watched by the controller.
	ObjectImportAlias string
	// StateFullType is a fully qualified type of the object holding
	// a state of the generated CRUD resource. It can be a pointer. E.g.
	// "github.com/user/repo/pkg.MyType".
	StateFullType string
	// StateImportAlias of the object reconciled by the generated resource.
	StateImportAlias string
}
