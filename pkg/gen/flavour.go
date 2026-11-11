package gen

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/pflag"
)

const (
	FlavourApp                     Flavour = "app"
	FlavourCLI                     Flavour = "cli"
	FlavourCustomer                Flavour = "customer"
	FlavourFork                    Flavour = "fork"
	FlavourGeneric                 Flavour = "generic"
	FlavourKubernetesAPI           Flavour = "k8sapi"
	FlavourClusterApp              Flavour = "cluster-app"
	FlavourManagementClustersFleet Flavour = "fleet"
	// FlavourPlans is a team plans repository: versioned PRDs, their
	// companion websites and the plan-workflow agent skills. Declared on its
	// own (flavours: [plans]) with language generic: the repository set-up
	// engine derives it the template giantswarm/template-plans, and gen plans
	// writes its shared set-up.
	FlavourPlans Flavour = "plans"
	// FlavourMultiImage is a repository of several images, each with its own
	// release line: one image per toolchains/<name>/ directory holding a
	// Dockerfile, released as <name>/vX.Y.Z. gen circleci renders its
	// per-image pipeline and gen workflows its per-directory Auto Release.
	FlavourMultiImage Flavour = "multi-image"
)

// MultiImageDir is the directory of a multi-image repository whose
// subdirectories holding a Dockerfile are its images: toolchains/go/Dockerfile
// is the image giantswarm/<repo>/go, released as go/vX.Y.Z. It is also the
// build context of every image, so the images share the build files kept
// there beside them, and a change to those releases every image.
const MultiImageDir = "toolchains"

func AllFlavours() []string {
	return []string{
		FlavourApp.String(),
		FlavourCLI.String(),
		FlavourCustomer.String(),
		FlavourFork.String(),
		FlavourGeneric.String(),
		FlavourKubernetesAPI.String(),
		FlavourClusterApp.String(),
		FlavourManagementClustersFleet.String(),
		FlavourPlans.String(),
		FlavourMultiImage.String(),
	}
}

// flavourDescriptions documents each flavour for the command help. Every entry
// of AllFlavours() must have one -- Test_FlavourDescriptions enforces that, so a
// new flavour cannot silently leave the help text behind.
var flavourDescriptions = map[Flavour]string{
	FlavourApp:                     "project containing a helm chart",
	FlavourCLI:                     "project released with a downloadable binary",
	FlavourCustomer:                "project whose issues are tracked on the general customer board",
	FlavourFork:                    "project carrying an upstream release plus patches on an organisation branch",
	FlavourGeneric:                 "everything else, i.e a project which simply needs to be released",
	FlavourKubernetesAPI:           "project containing a Kubernetes API",
	FlavourClusterApp:              "project containing helm chart, that is a cluster app (e.g. cluster-aws, cluster-azure, ...)",
	FlavourManagementClustersFleet: "project containing clusters using GitOps",
	FlavourPlans:                   "team plans repository holding PRDs, companion websites and plan-workflow skills",
	FlavourMultiImage:              "project building and releasing multiple images, one per toolchains/<name>/ directory",
}

// FlavourDescriptionList renders the flavours as an indented bullet list for a
// command's long description, in AllFlavours() order.
func FlavourDescriptionList() string {
	var b strings.Builder
	for _, f := range AllFlavours() {
		fmt.Fprintf(&b, "  - %s - %s\n", f, flavourDescriptions[Flavour(f)])
	}
	return b.String()
}

type Flavour string

func NewFlavour(s string) (Flavour, error) {
	switch s {
	case FlavourApp.String():
		return FlavourApp, nil
	case FlavourCLI.String():
		return FlavourCLI, nil
	case FlavourCustomer.String():
		return FlavourCustomer, nil
	case FlavourFork.String():
		return FlavourFork, nil
	case FlavourGeneric.String():
		return FlavourGeneric, nil
	case FlavourKubernetesAPI.String():
		return FlavourKubernetesAPI, nil
	case FlavourClusterApp.String():
		return FlavourClusterApp, nil
	case FlavourManagementClustersFleet.String():
		return FlavourManagementClustersFleet, nil
	case FlavourPlans.String():
		return FlavourPlans, nil
	case FlavourMultiImage.String():
		return FlavourMultiImage, nil
	}

	return Flavour("unknown"), microerror.Maskf(invalidConfigError, "flavour must be one of %s", strings.Join(AllFlavours(), "|"))
}

func (f Flavour) String() string {
	return string(f)
}

type FlavourSlice []Flavour

func (s FlavourSlice) Contains(f Flavour) bool {
	for _, x := range s {
		if x == f {
			return true
		}
	}
	return false
}

// Generates says whether the generators have anything to produce for the
// flavours. A fork line carries its upstream's files plus the carried
// patches: nothing is generated for it, align-files leaves it as it is; only
// `devctl gen renovate --flavour fork`, run by hand, writes the renovate.json
// Renovate needs to scan a GitHub fork.
func (s FlavourSlice) Generates() bool {
	return !s.Contains(FlavourFork)
}

type FlavourSliceFlagValue struct {
	value   *FlavourSlice
	changed bool
}

var _ pflag.Value = new(FlavourSliceFlagValue)
var _ pflag.SliceValue = new(FlavourSliceFlagValue)

func NewFlavourSliceFlagValue(p *FlavourSlice, value FlavourSlice) *FlavourSliceFlagValue {
	*p = value
	return &FlavourSliceFlagValue{
		value: p,
	}
}

func (s *FlavourSliceFlagValue) Set(val string) error {
	ss := strings.Split(val, ",")
	out := make([]Flavour, len(ss))
	for i, v := range ss {
		var err error
		out[i], err = NewFlavour(v)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	if !s.changed {
		*s.value = out
	} else {
		*s.value = append(*s.value, out...)
	}

	s.changed = true
	return nil
}

func (s *FlavourSliceFlagValue) Type() string {
	return "flavourSlice"
}

func (s *FlavourSliceFlagValue) String() string {
	out := make([]string, len(*s.value))
	for i, x := range *s.value {
		out[i] = x.String()
	}
	return "[" + strings.Join(out, ",") + "]"
}

func (s *FlavourSliceFlagValue) Append(val string) error {
	x, err := NewFlavour(val)
	if err != nil {
		return microerror.Mask(err)
	}

	*s.value = append(*s.value, x)
	return nil
}

func (s *FlavourSliceFlagValue) Replace(val []string) error {
	out := make([]Flavour, len(val))
	for i, x := range val {
		var err error
		out[i], err = NewFlavour(x)
		if err != nil {
			return microerror.Mask(err)
		}
	}
	*s.value = out
	return nil
}

func (s *FlavourSliceFlagValue) GetSlice() []string {
	out := make([]string, len(*s.value))
	for i, x := range *s.value {
		out[i] = x.String()
	}
	return out
}
