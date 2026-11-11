package params

import "github.com/giantswarm/devctl/v8/pkg/gen"

func IsLanguageGo(p Params) bool {
	return p.Language == "go"
}

func IsFlavourApp(p Params) bool {
	return p.Flavours.Contains(gen.FlavourApp)
}
