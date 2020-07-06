package params

import (
	"sort"
)

const (
	RootCmdDir = "cmd"
)

func Subcommands(p Params) []Subcommand {
	sort.Slice(p.Subcommands, func(i, j int) bool {
		return p.Subcommands[i].Alias < p.Subcommands[j].Alias
	})

	return p.Subcommands
}
