package gen

import (
	"strings"

	"github.com/giantswarm/microerror"
)

const (
	FlavourApp      Flavour = "app"
	FlavourCLI      Flavour = "cli"
	FlavourLibrary  Flavour = "library"
	FlavourOperator Flavour = "operator"
)

func AllFlavours() []string {
	return []string{
		FlavourApp.String(),
		FlavourCLI.String(),
		FlavourLibrary.String(),
		FlavourOperator.String(),
	}
}

type Flavour string

func NewFlavour(s string) (Flavour, error) {
	switch s {
	case FlavourApp.String():
		return FlavourApp, nil
	case FlavourCLI.String():
		return FlavourCLI, nil
	case FlavourLibrary.String():
		return FlavourLibrary, nil
	case FlavourOperator.String():
		return FlavourOperator, nil
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
