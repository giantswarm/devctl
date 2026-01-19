package gen

import (
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/pflag"
)

const (
	LanguageGo            Language = "go"
	LanguagePython        Language = "python"
	LanguageGeneric       Language = "generic"
	LanguageKyvernoPolicy Language = "kyverno-policy"
)

func AllLanguages() []string {
	return []string{
		LanguageGo.String(),
		LanguagePython.String(),
		LanguageGeneric.String(),
		LanguageKyvernoPolicy.String(),
	}
}

type Language string

func NewLanguage(s string) (Language, error) {
	switch s {
	case LanguageGo.String():
		return LanguageGo, nil
	case LanguagePython.String():
		return LanguagePython, nil
	case LanguageGeneric.String():
		return LanguageGeneric, nil
	case LanguageKyvernoPolicy.String():
		return LanguageKyvernoPolicy, nil
	}

	return Language("unknown"), microerror.Maskf(invalidConfigError, "language must be one of %s", strings.Join(AllLanguages(), "|"))
}

func (f Language) String() string {
	return string(f)
}

func IsValidLanguage(s string) bool {
	_, err := NewLanguage(s)
	return err == nil
}

type LanguageFlagValue Language

var _ pflag.Value = new(LanguageFlagValue)

func NewLanguageFlagValue(p *Language, value Language) *LanguageFlagValue {
	*p = value
	return (*LanguageFlagValue)(p)
}

func (v *LanguageFlagValue) Set(s string) error {
	x, err := NewLanguage(s)
	if err != nil {
		return microerror.Mask(err)
	}

	*v = LanguageFlagValue(x)
	return nil
}

func (v *LanguageFlagValue) Type() string {
	return "language"
}

func (v *LanguageFlagValue) String() string {
	return string(*v)
}
