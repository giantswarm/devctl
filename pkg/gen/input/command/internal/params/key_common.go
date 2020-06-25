package params

import (
	"fmt"
	"strings"

	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func IsRoot(p Params) bool {
	return Package(p) == "cmd"
}

func Name(p Params) string {
	return internal.Package(p.Name)
}

func Package(p Params) string {
	return internal.Package(p.Dir)
}

func Parent(p Params) string {
	if IsRoot(p) {
		return "PARENT_SHOULD_NOT_BE_USED_FOR_ROOT"
	}

	split := strings.Split(p.Dir, "/")
	if len(split) < 2 {
		panic(fmt.Sprintf("expected dir=%q to have at least 2 segments separated with %q, but got %d", p.Dir, "/", len(split)))
	}

	return split[len(split)-2]
}

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName(p.Dir, suffix)
}
