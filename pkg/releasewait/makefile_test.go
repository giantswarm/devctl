package releasewait

import (
	"slices"
	"testing"
)

func TestMakefilePushTargets(t *testing.T) {
	mk := parseMakefile([]byte(makePushMakefile))
	cases := []struct {
		name   string
		target string
		vars   map[string]string
		want   []string
	}{
		{"command line over the Makefile", "helm-publish", map[string]string{"HELM_REPO": "oci://gsoci.azurecr.io/giantswarm"}, []string{"gsoci.azurecr.io/giantswarm/kagent/helm", "gsoci.azurecr.io/giantswarm/kagent/helm"}},
		{"the Makefile's default", "helm-publish", nil, []string{"ghcr.io/kagent-dev/kagent/helm", "ghcr.io/kagent-dev/kagent/helm"}},
		{"a target that pushes nothing", "helm-version", nil, nil},
		{"an unknown target", "nope", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := mk.pushTargets(tc.target, tc.vars)
			if !slices.Equal(got, tc.want) {
				t.Errorf("pushTargets(%s) = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}

func TestMakeInvocations(t *testing.T) {
	commands := "registry=gsoci.azurecr.io\nVERSION=$(cat .build_version)\n" +
		`eval "$(make -s substrate-pin)"` + "\n" +
		`make helm-publish HELM_REPO="oci://${registry}/giantswarm" TAG=${VERSION}` + "\n" +
		"make -C other helm-publish\ncmake build\n"
	env := jobEnvironment(map[string]string{"VERSION": "x"}, commands)
	if env["registry"] != "gsoci.azurecr.io" {
		t.Errorf("registry: want the literal assignment, got %q", env["registry"])
	}
	if _, ok := env["VERSION"]; ok {
		t.Errorf("VERSION: a computed assignment leaves it unknown, got %q", env["VERSION"])
	}
	calls := makeInvocations(commands, env)
	if len(calls) != 2 {
		t.Fatalf("want the two calls of the root Makefile, got %+v", calls)
	}
	if !slices.Equal(calls[0].targets, []string{"substrate-pin"}) {
		t.Errorf("first call: %+v", calls[0])
	}
	if !slices.Equal(calls[1].targets, []string{"helm-publish"}) || calls[1].vars["HELM_REPO"] != "oci://gsoci.azurecr.io/giantswarm" || calls[1].vars["TAG"] != "" {
		t.Errorf("second call: %+v", calls[1])
	}
}
