package params

func HasFlavor(p Params, flavor string) bool {
	for _, f := range p.Flavors {
		if f == flavor {
			return true
		}
	}
	return false
}
