package filepathx

import (
	"regexp"

	"github.com/giantswarm/microerror"
)

type Glob struct {
	regexexp *regexp.Regexp
}

func Compile(pattern string) (*Glob, error) {
	if pattern == "" {
		return nil, microerror.Maskf(invalidConfigError, "pattern must not be empty")
	}

	// TODO TDD approach
	// TODO add stuff from the comment https://github.com/giantswarm/giantswarm/issues/7021#issuecomment-546902876

	g := &Glob{
		regexp: nil,
	}

	return g, nil
}

func MatchString(s string) bool {
	return false
}
