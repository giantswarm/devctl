package input

import (
	"io/fs"
	"runtime/debug"
	"strings"
)

const devctlModule = "github.com/giantswarm/devctl/v8"

// TemplateSHA is the provenance link a generated file's header carries for
// the template name: the content of name.sha next to the template, which
// `go generate` writes from the last commit that touched the template
// (update-template-sha.go) and which is gitignored. In a build of devctl as a
// module dependency the .sha files are absent — the module zip holds only
// what is committed — and the link points at the module version's tree
// instead, so the gen packages compile from the module proxy.
func TemplateSHA(files fs.FS, name string) string {
	if b, err := fs.ReadFile(files, name+".sha"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	return "https://github.com/giantswarm/devctl/tree/" + moduleRef()
}

// moduleRef is the devctl version of the build: the main module's when devctl
// itself is built, the dependency's when devctl is imported, main otherwise.
func moduleRef() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "main"
	}
	if bi.Main.Path == devctlModule && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	for _, d := range bi.Deps {
		if d.Path != devctlModule {
			continue
		}
		if d.Replace != nil && d.Replace.Version != "" {
			return d.Replace.Version
		}
		if d.Version != "" {
			return d.Version
		}
	}
	return "main"
}
