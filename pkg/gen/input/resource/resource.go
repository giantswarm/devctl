package resource

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

type Config struct {
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

func (c *Config) Validate() error {
	if c.Dir == "" {
		return microerror.Maskf(invalidConfigError, "%T.Dir must not be empty", c)
	}
	if c.ObjectFullType == "" {
		return microerror.Maskf(invalidConfigError, "%T.ObjectFullType must not be empty", c)
	}
	if c.StateFullType == "" {
		return microerror.Maskf(invalidConfigError, "%T.StateFullType must not be empty", c)
	}

	return nil
}

type Resource struct {
	params params.Params
}

func New(config Config) (*Resource, error) {
	err := config.Validate()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	r := &Resource{
		params: params.Params(config),
	}

	return r, nil
}

func (r *Resource) CreateFile() input.Input  { return file.NewCreateInput(r.params) }
func (r *Resource) CurrentFile() input.Input { return file.NewCurrentInput(r.params) }
func (r *Resource) DeleteFile() input.Input  { return file.NewDeleteInput(r.params) }
func (r *Resource) DesiredFile() input.Input { return file.NewDesiredInput(r.params) }
func (r *Resource) ErrorFile() input.Input   { return file.NewErrorInput(r.params) }
func (r *Resource) KeyFile() input.Input     { return file.NewKeyInput(r.params) }
func (r *Resource) PatchFile() input.Input   { return file.NewPatchInput(r.params) }
func (r *Resource) UpdateFile() input.Input  { return file.NewUpdateInput(r.params) }
