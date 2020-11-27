package gen

import (
	"strings"

	"github.com/giantswarm/microerror"
)

const (
	FlavourApp     Flavour = "app"
	FlavourCLI     Flavour = "cli"
	FlavourGeneric Flavour = "generic"
)

type Flavour string

func NewFlavour(s string) (Flavour, error) {
	switch s {
	case FlavourApp.String():
		return FlavourApp, nil
	case FlavourCLI.String():
		return FlavourCLI, nil
	case FlavourGeneric.String():
		return FlavourGeneric, nil
	}

	return Flavour("unknown"), microerror.Maskf(invalidConfigError, "flavour must be one of %s", strings.Join(AllFlavours(), "|"))
}

func (f Flavour) String() string {
	return string(f)
}

type Flavours []Flavour

func AllFlavours() Flavours {
	return Flavours{
		FlavourApp,
		FlavourCLI,
		FlavourGeneric,
	}
}

func (fs Flavours) ToStringSlice() []string {
	var ss []string
	for _, f := range fs {
		ss = append(ss, f.String())
	}
	return ss
}

func IsValidFlavour(s string) bool {
	_, err := NewFlavour(s)
	return err == nil
}
