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

func AllFlavours() []string {
	return []string{
		FlavourApp.String(),
		FlavourCLI.String(),
		FlavourGeneric.String(),
	}
}

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

func IsValidFlavour(s string) bool {
	_, err := NewFlavour(s)
	return err == nil
}
