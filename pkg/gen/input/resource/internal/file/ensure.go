package file

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
	"github.com/giantswarm/devctl/pkg/xstrings"
)

type Ensure struct {
	dir           string
	objectGroup   string
	objectKind    string
	objectVersion string
}

func NewEnsure(p params.Params) *Ensure {
	f := &Ensure{
		dir:           p.Dir,
		objectGroup:   p.ObjectGroup,
		objectKind:    p.ObjectKind,
		objectVersion: p.ObjectVersion,
	}

	return f
}

func (f *Ensure) GetInput(ctx context.Context) (input.Input, error) {
	i := input.Input{
		Path:         filepath.Join(f.dir, params.RegenerableFileName("ensure.go")),
		Scaffolding:  false,
		TemplateBody: ensureTemplate,
		TemplateData: map[string]interface{}{
			"ClientImport":       params.ClientImport(f.objectGroup),
			"ClientPackage":      params.ClientPackage(f.objectGroup),
			"ObjectGroup":        f.objectGroup,
			"ObjectGroupTitle":   strings.Title(f.objectGroup),
			"ObjectImport":       params.ObjectImport(f.objectGroup, f.objectVersion),
			"ObjectKind":         f.objectKind,
			"ObjectKindLower":    xstrings.FirstLetterToLower(f.objectKind),
			"ObjectVersion":      f.objectVersion,
			"ObjectVersionTitle": strings.Title(f.objectVersion),
			"Package":            params.Package(f.dir),
		},
	}

	return i, nil
}

var ensureTemplate = `package {{ .Package }}

import (
	"context"
	"fmt"

	"github.com/giantswarm/microerror"
	{{ .ObjectGroup }}{{ .ObjectVersion }} "{{ .ObjectImport }}"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *Resource) ensure(ctx context.Context, current, desired []*{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}) error {
	var toCreate []*{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}
	var toUpdate []*{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}
	var toDelete []*{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}
	{
		for _, d := range desired {
			idx, ok := find(current, d)
			if ok {
				u := new{{ .ObjectKind }}ToUpdate(ctx, current[idx], d)
				if u != nil {
					toUpdate = append(toUpdate, u)
				}
			} else {
				toCreate = append(toCreate, d)
			}
		}

		for _, c := range current {
			_, ok := find(desired, c)
			if ok {
				// Skip. This object is still "desired".
			} else {
				toDelete = append(toDelete, c)
			}
		}
	}

	// Create {{ .ObjectKind }} objects that don't exist.
	{
		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("creating %d {{ .ObjectKind }} objects", len(toCreate)))

		for i, o := range toCreate {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("creating {{ .ObjectKind }} %#q in namespace %#q (%d/%d)", o.GetName(), o.GetNamespace(), i+1, len(toCreate)))

			_, err := r.k8sClient.{{ .ObjectGroupTitle }}{{ .ObjectVersionTitle }}().{{ .ObjectKind }}s(o.GetNamespace()).Create(o)
			if err != nil {
				return microerror.Mask(err)
			}

			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("created {{ .ObjectKind }} %#q in namespace %#q (%d/%d)", o.GetName(), o.GetNamespace(), i+1, len(toCreate)))
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("created %d {{ .ObjectKind }} objects", len(toCreate)))
	}

	// Update outdated {{ .ObjectKind }} objects.
	{
		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("updating %d {{ .ObjectKind }} objects", len(toUpdate)))

		for i, o := range toUpdate {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("updating {{ .ObjectKind }} %#q in namespace %#q (%d/%d)", o.GetName(), o.GetNamespace(), i+1, len(toUpdate)))

			_, err := r.k8sClient.{{ .ObjectGroupTitle }}{{ .ObjectVersionTitle }}().{{ .ObjectKind }}s(o.GetNamespace()).Update(o)
			if err != nil {
				return microerror.Mask(err)
			}

			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("updated {{ .ObjectKind }} %#q in namespace %#q (%d/%d)", o.GetName(), o.GetNamespace(), i+1, len(toUpdate)))
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("updated %d {{ .ObjectKind }} objects", len(toUpdate)))
	}

	// Delete {{ .ObjectKind }} objects that are not longer desired.
	{
		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("deleting %d {{ .ObjectKind }} objects", len(toDelete)))

		for i, o := range toDelete {
			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("deleting {{ .ObjectKind }} %#q in namespace %#q (%d/%d)", o.GetName(), o.GetNamespace(), i+1, len(toDelete)))

			err := r.k8sClient.{{ .ObjectGroupTitle }}{{ .ObjectVersionTitle }}().{{ .ObjectKind }}s(o.GetNamespace()).Delete(o.GetName(), &metav1.DeleteOptions{})
			if err != nil {
				return microerror.Mask(err)
			}

			r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("deleted {{ .ObjectKind }} %#q in namespace %#q (%d/%d)", o.GetName(), o.GetNamespace(), i+1, len(toDelete)))
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("deleted %d {{ .ObjectKind }} objects", len(toDelete)))
	}

	return nil

}
`
