package gen

import (
	"strings"

	"github.com/giantswarm/microerror"
)

const (
	LanguageGo      Language = "go"
	LanguageGeneric Language = "generic"
)

func AllLanguages() []string {
	return []string{
		LanguageGo.String(),
	}
}

type Language string

func NewLanguage(s string) (Language, error) {
	switch s {
	case LanguageGo.String():
		return LanguageGo, nil
	}

	return Language("unknown"), microerror.Maskf(invalidConfigError, "flavour must be one of %s", strings.Join(AllLanguages(), "|"))
}

func (f Language) String() string {
	return string(f)
}

func IsValidLanguage(s string) bool {
	_, err := NewLanguage(s)
	return err == nil
}
