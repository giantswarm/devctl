package input

import (
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
