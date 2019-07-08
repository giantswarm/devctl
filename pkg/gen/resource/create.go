package resource

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/microerror"
)

type Create struct {
	dir           string
	objectGroup   string
	objectKind    string
	objectVersion string
}

func NewCreate(config Config) (*Create, error) {
	err := config.Validate()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	f := &Create{
		dir:           config.Dir,
		objectGroup:   config.ObjectGroup,
		objectKind:    config.ObjectKind,
		objectVersion: config.ObjectVersion,
	}

	return f, nil
}

func (f *Create) GetInput(ctx context.Context) (input.Input, error) {
	i := input.Input{
		Path:         filepath.Join(f.dir, "create.go"),
		TemplateBody: createTemplate,
		TemplateData: map[string]interface{}{
			"ObjectGroup":        f.objectGroup,
			"ObjectKind":         f.objectKind,
			"ObjectVersion":      f.objectVersion,
			"Package":            f.dir,
			"ObjectGroupTitle":   strings.Title(f.objectGroup),
			"ObjectVersionTitle": strings.Title(f.objectVersion),
		},
	}

	return i, nil
}

var createTemplate = `package {{ .Package }}

import (
	"context"
	"fmt"

	"github.com/giantswarm/microerror"
	{{ .ObjectGroup }}{{ .ObjectVersion }} "k8s.io/api/{{ .ObjectGroup }}/{{ .ObjectVersion }}"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ApplyCreateChange ensures the {{ .ObjectKind }} is created in the k8s api.
func (r *Resource) ApplyCreateChange(ctx context.Context, obj, createChange interface{}) error {
	configMaps, err := to{{ .ObjectKind }}s(createChange)
	if err != nil {
		return microerror.Mask(err)
	}

	for _, configMap := range configMaps {
		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("creating {{ .ObjectKind }} %#q in namespace %#q", configMap.Name, configMap.Namespace))

		_, err = r.k8sClient.{{ .ObjectGroupTitle }}{{ .ObjectVersionTitle }}().{{ .ObjectKind }}s(configMap.Namespace).Create(configMap)
		if apierrors.IsAlreadyExists(err) {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("already created {{ .ObjectKind }} %#q in namespace %#q", configMap.Name, configMap.Namespace))
		} else if err != nil {
			return microerror.Mask(err)
		} else {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("created {{ .ObjectKind }} %#q in namespace %#q", configMap.Name, configMap.Namespace))
		}
	}

	return nil
}

func (r *Resource) newCreateChange(ctx context.Context, obj, currentState, desiredState interface{}) (interface{}, error) {
	currentConfigMaps, err := to{{ .ObjectKind }}s(currentState)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	desiredConfigMaps, err := to{{ .ObjectKind }}s(desiredState)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var configMapsToCreate []*{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}
	{
		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("computing {{ .ObjectKind }}s to create "))

		for _, d := range desiredConfigMaps {
			if !contains{{ .ObjectKind }}(currentConfigMaps, d) {
				configMapsToCreate = append(configMapsToCreate, d)
			}
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("computed %d {{ .ObjectKind }}s to create", len(configMapsToCreate)))
	}

	return configMapsToCreate, nil
}
`
