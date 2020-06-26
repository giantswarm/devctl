package params

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func Subcommands(p Params) []Subcommand {
	// TODO(PK): I need to think about something smarter here. It would be good to be able to generate that outside giantswarm.
	module := filepath.Join("github.com", "giantswarm", Name(p))

	var subcommands []Subcommand
	walkCommandsTree(p, func(c ParamsCommandTree, parents []string) {
		if len(parents) == 0 {
			return
		}

		parentAlias := strings.Join(parents[1:], "")
		if len(parents) == 1 {
			parentAlias = "root"
		}

		s := Subcommand{
			Alias:       strings.Join(parents[1:], "") + c.Name,
			Import:      filepath.Join(module, filepath.Join(parents...), c.Name),
			ParentAlias: parentAlias,
		}

		subcommands = append(subcommands, s)
	})

	sort.Slice(subcommands, func(i, j int) bool {
		return subcommands[i].Alias < subcommands[j].Alias
	})

	return subcommands
}

func Name(p Params) string {
	return internal.Package(p.Name)
}

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName("", suffix)
}

func walkCommandsTree(p Params, walkFunc func(tree ParamsCommandTree, parents []string)) {
	walkCommandsTreeAux(p.RootCommand, []string{}, walkFunc)
}

func walkCommandsTreeAux(tree ParamsCommandTree, parents []string, walkFunc func(tree ParamsCommandTree, parents []string)) {
	walkFunc(tree, parents)
	parents = append(parents, tree.Name)

	for _, c := range tree.Subcommands {
		walkCommandsTreeAux(c, parents, walkFunc)
	}
}
