package gen

import (
	"strings"

	"github.com/giantswarm/microerror"
)

const (
	EcosystemDocker        Ecosystem = "docker"
	EcosystemGithubActions Ecosystem = "github-actions"
	EcosystemGomod         Ecosystem = "gomod"
	EcosystemNPM           Ecosystem = "npm"
	EcosystemPIP           Ecosystem = "pip"
)

func AllowedEcosystems() []string {
	return []string{
		EcosystemDocker.String(),
		EcosystemGithubActions.String(),
		EcosystemGomod.String(),
		EcosystemNPM.String(),
		EcosystemPIP.String(),
	}
}

type Ecosystem string

func NewEcosystem(s string) (Ecosystem, error) {
	switch s {
	case EcosystemDocker.String():
		return EcosystemDocker, nil
	case EcosystemGithubActions.String():
		return EcosystemGithubActions, nil
	case EcosystemGomod.String():
		return EcosystemGomod, nil
	case EcosystemNPM.String():
		return EcosystemNPM, nil
	case EcosystemPIP.String():
		return EcosystemPIP, nil
	}

	return Ecosystem("unknown"), microerror.Maskf(invalidConfigError, "ecosystem must be one of %s", strings.Join(AllowedEcosystems(), "|"))
}

func (e Ecosystem) String() string {
	return string(e)
}

func IsValidEcoSystem(ecosystems []string) bool {
	for _, s := range ecosystems {
		_, err := NewEcosystem(s)
		if err != nil {
			return false
		}
	}
	return true
}
