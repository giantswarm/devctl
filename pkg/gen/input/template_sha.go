package input

import (
	"io/fs"
	"runtime/debug"
	"strings"

	"golang.org/x/mod/module"
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
// Pseudo-versions refer to their commit, rather than a nonexistent Git tag.
func moduleRef() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "main"
	}
	return moduleRefFromBuildInfo(bi)
}

func moduleRefFromBuildInfo(bi *debug.BuildInfo) string {
	m := &bi.Main
	if m.Path != devctlModule {
		m = nil
		for _, d := range bi.Deps {
			if d.Path == devctlModule {
				m = d
				break
			}
		}
	}
	if m == nil {
		return "main"
	}
	if m.Replace != nil {
		m = m.Replace
	}
	// An unversioned replacement is local source. Its original requirement's
	// version does not identify the code that was actually built.
	if m.Version == "" || m.Version == "(devel)" {
		return "main"
	}
	if rev, err := module.PseudoVersionRev(m.Version); err == nil {
		return rev
	}
	return m.Version
}
