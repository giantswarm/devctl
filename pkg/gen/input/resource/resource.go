package resource

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
	"github.com/giantswarm/devctl/pkg/xstrings"
)

type Config struct {
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

func (c *Config) Validate() error {
	validObjectGroups := []string{
		"core",
		"g8s",
	}

	if c.Dir == "" {
		return microerror.Maskf(invalidConfigError, "%T.Dir must not be empty", c)
	}
	if c.ObjectGroup == "" {
		c.ObjectGroup = "core"
	}
	if !xstrings.Contains(validObjectGroups, c.ObjectGroup) {
		return microerror.Maskf(invalidConfigError, "%T.ObjectGroup must one of %v but got %#q", c, validObjectGroups, c.ObjectGroup)
	}
	if c.ObjectKind == "" {
		return microerror.Maskf(invalidConfigError, "%T.ObjectKind must not be empty", c)
	}
	if c.ObjectVersion == "" {
		return microerror.Maskf(invalidConfigError, "%T.ObjectVersion must not be empty", c)
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

func (r *Resource) CreateFile() input.File   { return file.NewCreate(r.params) }
func (r *Resource) CurrentFile() input.File  { return file.NewCurrent(r.params) }
func (r *Resource) ResourceFile() input.File { return file.NewResource(r.params) }
