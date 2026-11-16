package checkextras

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const block = `patches:
  # BEGIN reservation of app, written by ` + "`devctl reservation reserve`" + `. Do not edit.
  - target:
      kind: OCIRepository
      name: app
    patch: |-
      - op: add
        path: /spec/ref
        value: {semver: ">=0.0.0-0"}
  # END reservation of app, written by ` + "`devctl reservation reserve`" + `
`

const humanPatch = `  - target:
      kind: OCIRepository
      name: app
    patch: |-
      - op: replace
        path: /spec/ref/semver
        value: ">=2.0.0"
`

func TestCheckExtras(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		ok      bool
	}{
		"no patches":          {"resources:\n  - ./app/\n", true},
		"block last":          {"resources:\n  - ./app/\n" + block, true},
		"patch after a block": {"resources:\n  - ./app/\n" + block + humanPatch, false},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kustomization.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			cmd, err := New(Config{Stdout: &stdout})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetArgs([]string{path})
			cmd.SilenceUsage, cmd.SilenceErrors = true, true

			err = cmd.Execute()
			if tc.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tc.ok && (err == nil || !strings.Contains(err.Error(), path)) {
				t.Errorf("expected an error naming %s, got %v", path, err)
			}
		})
	}
}
