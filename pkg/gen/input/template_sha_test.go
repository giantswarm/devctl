package input

import (
	"runtime/debug"
	"strings"
	"testing"
	"testing/fstest"
)

func TestTemplateSHA(t *testing.T) {
	withSHA := fstest.MapFS{
		"x.yaml.template":     {Data: []byte("template")},
		"x.yaml.template.sha": {Data: []byte("https://github.com/giantswarm/devctl/blob/abc/pkg/x.yaml.template\n")},
	}
	if got := TemplateSHA(withSHA, "x.yaml.template"); got != "https://github.com/giantswarm/devctl/blob/abc/pkg/x.yaml.template" {
		t.Errorf("with .sha: %q", got)
	}
	withoutSHA := fstest.MapFS{"x.yaml.template": {Data: []byte("template")}}
	if got := TemplateSHA(withoutSHA, "x.yaml.template"); !strings.HasPrefix(got, "https://github.com/giantswarm/devctl/tree/") {
		t.Errorf("without .sha: %q", got)
	}
}

// BuildInfo fixtures model the source identity carried by real module builds.
// A pseudo-version is a commit identity, while (devel) is not a Git ref.
func TestModuleRefFromBuildInfo(t *testing.T) {
	const pseudo = "v8.163.1-0.20261103120000-f4b312bbb50d"
	const prereleasePseudo = "v8.164.0-rc.1.0.20261103120000-f4b312bbb50d"
	const revision = "f4b312bbb50d"
	tests := []struct {
		name string
		main debug.Module
		deps []*debug.Module
		want string
	}{
		{name: "no devctl module", want: "main"},
		{name: "unrelated main", main: debug.Module{Path: "example.invalid/consumer", Version: "v1.2.3"}, want: "main"},
		{name: "tagged main", main: debug.Module{Path: devctlModule, Version: "v8.163.0"}, want: "v8.163.0"},
		{name: "prerelease main", main: debug.Module{Path: devctlModule, Version: "v8.164.0-rc.1"}, want: "v8.164.0-rc.1"},
		{name: "development main", main: debug.Module{Path: devctlModule, Version: "(devel)"}, want: "main"},
		{name: "unversioned main", main: debug.Module{Path: devctlModule}, want: "main"},
		{name: "pseudo-version main", main: debug.Module{Path: devctlModule, Version: pseudo}, want: revision},
		{name: "tagged dependency", deps: []*debug.Module{{Path: "example.invalid/other", Version: "v1.2.3"}, {Path: devctlModule, Version: "v8.163.0"}}, want: "v8.163.0"},
		{name: "pseudo-version dependency", deps: []*debug.Module{{Path: devctlModule, Version: pseudo}}, want: revision},
		{name: "prerelease pseudo-version dependency", deps: []*debug.Module{{Path: devctlModule, Version: prereleasePseudo}}, want: revision},
		{name: "development dependency", deps: []*debug.Module{{Path: devctlModule, Version: "(devel)"}}, want: "main"},
		{name: "unversioned dependency", deps: []*debug.Module{{Path: devctlModule}}, want: "main"},
		{name: "tagged replacement", deps: []*debug.Module{{Path: devctlModule, Version: "v8.160.0", Replace: &debug.Module{Path: devctlModule, Version: "v8.163.0"}}}, want: "v8.163.0"},
		{name: "pseudo-version replacement", deps: []*debug.Module{{Path: devctlModule, Version: "v8.160.0", Replace: &debug.Module{Path: devctlModule, Version: pseudo}}}, want: revision},
		{name: "local replacement", deps: []*debug.Module{{Path: devctlModule, Version: "v8.160.0", Replace: &debug.Module{Path: "../devctl", Version: "(devel)"}}}, want: "main"},
		{name: "empty replacement version", deps: []*debug.Module{{Path: devctlModule, Version: "v8.160.0", Replace: &debug.Module{Path: "../devctl"}}}, want: "main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := moduleRefFromBuildInfo(&debug.BuildInfo{Main: tt.main, Deps: tt.deps}); got != tt.want {
				t.Errorf("provenance ref = %q, want %q", got, tt.want)
			}
		})
	}
}
