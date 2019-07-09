package resource

import (
	"context"
	"path"
	"path/filepath"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
)

type Resource struct {
	dir           string
	objectGroup   string
	objectKind    string
	objectVersion string
}

func NewResource(config Config) (*Resource, error) {
	err := config.Validate()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	f := &Resource{
		dir:           config.Dir,
		objectGroup:   config.ObjectGroup,
		objectKind:    config.ObjectKind,
		objectVersion: config.ObjectVersion,
	}

	return f, nil
}

func (f *Resource) GetInput(ctx context.Context) (input.Input, error) {
	var clientImport string
	{
		switch f.objectGroup {
		case "core":
			clientImport = "k8s.io/client-go/kubernetes"
		default:
			return input.Input{}, microerror.Maskf(executionFailedError, "determine client import for group %#q", f.objectGroup)
		}
	}

	i := input.Input{
		Path:         filepath.Join(f.dir, "resource.go"),
		TemplateBody: resourceTemplate,
		TemplateData: map[string]interface{}{
			"ClientImport":    clientImport,
			"ClientPackage":   path.Base(clientImport),
			"ObjectGroup":     f.objectGroup,
			"ObjectKind":      f.objectKind,
			"ObjectKindLower": firstLetterToLower(f.objectKind),
			"ObjectVersion":   f.objectVersion,
			"Package":         f.dir,
		},
	}

	return i, nil
}

var resourceTemplate = `package {{ .Package }}

import (
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	{{ .ObjectGroup }}{{ .ObjectVersion }} "k8s.io/api/{{ .ObjectGroup }}/{{ .ObjectVersion }}"
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

func contains{{ .ObjectKind }}({{ .ObjectKindLower }}s []*{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}, {{ .ObjectKindLower }} *{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}) bool {
	for _, a := range {{ .ObjectKindLower }}s {
		if {{ .ObjectKindLower }}.Name == a.Name && {{ .ObjectKindLower }}.Namespace == a.Namespace {
			return true
		}
	}

	return false
}

func to{{ .ObjectKind }}s(v interface{}) ([]*{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}, error) {
	x, ok := v.([]*{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }})
	if !ok {
		return nil, microerror.Maskf(wrongTypeError, "expected '%T', got '%T'", x, v)
	}

	return x, nil
}
`
