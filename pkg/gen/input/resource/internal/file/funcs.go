package file

import (
	"context"
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/resource/internal/params"
)

type Funcs struct {
	dir           string
	objectGroup   string
	objectKind    string
	objectVersion string
}

func NewFuncs(p params.Params) *Funcs {
	f := &Funcs{
		dir:           p.Dir,
		objectGroup:   p.ObjectGroup,
		objectKind:    p.ObjectKind,
		objectVersion: p.ObjectVersion,
	}

	return f
}

func (f *Funcs) GetInput(ctx context.Context) (input.Input, error) {
	i := input.Input{
		Path:         filepath.Join(f.dir, "funcs.go"),
		Scaffolding:  false,
		TemplateBody: funcsTemplate,
		TemplateData: map[string]interface{}{
			"ObjectGroup":   f.objectGroup,
			"ObjectImport":  params.ObjectImport(f.objectGroup, f.objectVersion),
			"ObjectKind":    f.objectKind,
			"ObjectVersion": f.objectVersion,
			"Package":       params.Package(f.dir),
		},
	}

	return i, nil
}

var funcsTemplate = `package {{ .Package }}

import (
	"context"
	"reflect"

	{{ .ObjectGroup }}{{ .ObjectVersion }} "{{ .ObjectImport }}"
)

// new{{ .ObjectKind }}ToUpdate creates a new instance of {{ .ObjectKind }} ready to be used as an
// argument to Update method of generated client. It returns nil if the name or
// namespace doesn't match or if objects don't have differences in scope of
// interest.
func new{{ .ObjectKind }}ToUpdate(ctx context.Context, current, desired *{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }}) *{{ .ObjectGroup }}{{ .ObjectVersion }}.{{ .ObjectKind }} {
	merged := current.DeepCopy()

	panic("TODO implement")
	/*
		Here should go the code copying relevant parts (i.e. parts that need to be
		updated) from the desired object to the merged object. For a {{ .ObjectKind }} it may
		look like follows:

			merged.Annotations = desired.Annotations
			merged.Labels = desired.Labels

			merged.BinaryData = desired.BinaryData
			merged.Data = desired.Data
	*/

	// If the current and the merged object are still equal there is
	// nothing to be updated. Return nil.
	if reflect.DeepEqual(current, merged) {
		return nil
	}

	return merged
}
`
