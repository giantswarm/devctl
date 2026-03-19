package params

func Language(p Params) string {
	return p.Language
}

func HasFlavor(p Params, flavor string) bool {
	for _, f := range p.Flavors {
		if f == flavor {
			return true
		}
	}
	return false
}
