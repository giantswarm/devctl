package gen

import (
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/pflag"
)

const (
	FlavourApp                     Flavour = "app"
	FlavourCLI                     Flavour = "cli"
	FlavourCustomer                Flavour = "customer"
	FlavourGeneric                 Flavour = "generic"
	FlavourKubernetesAPI           Flavour = "k8sapi"
	FlavourClusterApp              Flavour = "cluster-app"
	FlavourManagementClustersFleet Flavour = "fleet"
)

func AllFlavours() []string {
	return []string{
		FlavourApp.String(),
		FlavourCLI.String(),
		FlavourCustomer.String(),
		FlavourGeneric.String(),
		FlavourKubernetesAPI.String(),
		FlavourClusterApp.String(),
		FlavourManagementClustersFleet.String(),
	}
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
	case FlavourGeneric.String():
		return FlavourGeneric, nil
	case FlavourKubernetesAPI.String():
		return FlavourKubernetesAPI, nil
	case FlavourClusterApp.String():
		return FlavourClusterApp, nil
	case FlavourManagementClustersFleet.String():
		return FlavourManagementClustersFleet, nil
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
