package file

import (
	"context"
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
	"github.com/giantswarm/devctl/pkg/xstrings"
)

type Resource struct {
	dir           string
	objectGroup   string
	objectKind    string
	objectVersion string
}

func NewResource(p params.Params) *Resource {
	f := &Resource{
		dir:           p.Dir,
		objectGroup:   p.ObjectGroup,
		objectKind:    p.ObjectKind,
		objectVersion: p.ObjectVersion,
	}

	return f
}

func (f *Resource) GetInput(ctx context.Context) (input.Input, error) {
	i := input.Input{
		Path:         filepath.Join(f.dir, params.RegenerableFileName("resource.go")),
		Scaffolding:  false,
		TemplateBody: resourceTemplate,
		TemplateData: map[string]interface{}{
			"ClientImport":    params.ClientImport(f.objectGroup),
			"ClientPackage":   params.ClientPackage(f.objectGroup),
			"ObjectGroup":     f.objectGroup,
			"ObjectImport":    params.ObjectImport(f.objectGroup, f.objectVersion),
			"ObjectKind":      f.objectKind,
			"ObjectKindLower": xstrings.FirstLetterToLower(f.objectKind),
			"ObjectVersion":   f.objectVersion,
			"Package":         params.Package(f.dir),
		},
	}

	return i, nil
}

var resourceTemplate = `package {{ .Package }}

import (
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"

	{{ .ObjectGroup }}{{ .ObjectVersion }} "{{ .ObjectImport }}"
	"{{ .ClientImport }}"
)

type Config struct {
	Client      {{ .ClientPackage }}.Interface
	Logger      micrologger.Logger
	StateGetter StateGetter

	Name string
}

type Resource struct {
	client      {{ .ClientPackage }}.Interface
	logger      micrologger.Logger
	stateGetter StateGetter

	name string
}

func New(config Config) (*Resource, error) {
	if config.Client == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Client must not be empty", config)
	}
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.StateGetter == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.StateGetter must not be empty", config)
	}

	if config.Name == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Name must not be empty", config)
	}

	r := &Resource{
		client:      config.Client,
		logger:      config.Logger,
		stateGetter: config.StateGetter,

		name: config.Name,
	}

	return r, nil
}

func (r *Resource) Name() string {
	return r.name
}

func find(resources []*{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}, r *{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}) (int, bool) {
	for i, resource := range resources {
		if resource.GetName() == r.GetName() && resource.GetNamespace() == r.GetNamespace() {
			return i, true
		}
	}

	return -1, false
}
`
