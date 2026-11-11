package params

import "github.com/giantswarm/devctl/v8/pkg/gen"

func IsLanguageGo(p Params) bool {
	return p.Language == "go"
}

func IsFlavourApp(p Params) bool {
	return p.Flavours.Contains(gen.FlavourApp)
}

// IsCodeRepository reports whether the repository holds application code or
// Kubernetes API types, as opposed to charts, configuration or policies only.
func IsCodeRepository(p Params) bool {
	switch gen.Language(p.Language) {
	case gen.LanguageGo, gen.LanguageNode, gen.LanguagePython:
		return true
	}
	return p.Flavours.Contains(gen.FlavourKubernetesAPI)
}
