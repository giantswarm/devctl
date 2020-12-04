package params

import (
	"sort"

	"github.com/giantswarm/devctl/pkg/gen"
)

func Ecosystems(p Params) []string {
	sort.Strings(p.Ecosystems)
	return p.Ecosystems
}

func EcosystemGomod(p Params) string {
	return gen.EcosystemGomod.String()
}

func Interval(p Params) string {
	return p.Interval
}

func Reviewers(p Params) []string {
	sort.Strings(p.Reviewers)
	return p.Reviewers
}
