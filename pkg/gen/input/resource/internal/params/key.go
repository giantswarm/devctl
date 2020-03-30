package params

import (
	"path/filepath"
	"strings"

	"github.com/giantswarm/devctl/pkg/gen/internal"
	"github.com/giantswarm/microerror"
)

func Dir(params Params) string {
	return params.Dir
}

func ObjectImport(params Params) string {
	return extractImport(params.ObjectFullType)
}

func ObjectImportAlias(params Params) string {
	return params.ObjectImportAlias
}

func ObjectType(params Params) string {
	t := extractType(params.ObjectFullType)
	if !strings.HasPrefix(t, "*") {
		err := microerror.Maskf(invalidConfigError, "expected the watched object type = %q to be a pointer", params.ObjectFullType)
		panic(microerror.Stack(err))
	}

	return strings.TrimLeft(t, "*")
}

func RegenerableFileName(params Params, suffix string) string {
	return filepath.Join(params.Dir, internal.RegenerableFilePrefix+suffix)
}

func ScaffoldingFileName(params Params, suffix string) string {
	return filepath.Join(params.Dir, suffix)
}

func StateImport(params Params) string {
	return extractImport(params.StateFullType)
}

func StateImportAlias(params Params) string {
	return params.StateImportAlias
}

func StateType(params Params) string {
	return extractType(params.StateFullType)
}

func Package(params Params) string {
	return filepath.Base(params.Dir)
}

// TODO test
func extractImport(fullType string) string {
	i := strings.LastIndex(fullType, ".")
	if i <= 0 {
		err := microerror.Maskf(invalidConfigError, "expected at least one \".\" character in full type = %q", fullType)
		panic(microerror.Stack(err))
	}
	if len(fullType) < i+1 {
		err := microerror.Maskf(invalidConfigError, "expected at least one character after last \".\" character in full type = %q", fullType)
		panic(microerror.Stack(err))
	}

	return strings.TrimLeft(fullType[:i+1], "*")
}

// TODO test
func extractType(fullType string) string {
	split := strings.Split(fullType, "/")
	if len(split) == 0 {
		err := microerror.Maskf(invalidConfigError, "expected at least one character after last \"/\" character in the fullType = %q", fullType)
		panic(microerror.Stack(err))
	}

	t := split[len(split)-1]
	if strings.HasPrefix(fullType, "*") {
		t = "*" + t
	}

	return t
}
