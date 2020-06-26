package params

import (
	"sort"

	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func Subcommands(p Params) []Subcommand {
	sort.Slice(p.Subcommands, func(i, j int) bool {
		return p.Subcommands[i].Alias < p.Subcommands[j].Alias
	})

	return p.Subcommands
}

func Name(p Params) string {
	return internal.Package(p.Name)
}

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName("", suffix)
}
