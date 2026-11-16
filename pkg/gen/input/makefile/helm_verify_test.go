package makefile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

func TestInstallHelmUnittestWithPipefail(t *testing.T) {
	for _, binary := range []string{"make", "bash", "cat"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is not installed", binary)
		}
	}

	makefile, err := New(Config{Flavours: gen.FlavourSlice{gen.FlavourApp}})
	require.NoError(t, err)
	input := makefile.MakefileGenApp()
	parsedTemplate, err := template.New("makefile").Parse(input.TemplateBody)
	require.NoError(t, err)
	var rendered bytes.Buffer
	require.NoError(t, parsedTemplate.Execute(&rendered, input.TemplateData))

	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, input.Path), rendered.Bytes(), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(directory, "bin"), 0o750))
	helpTail := filepath.Join(directory, "help-tail")
	require.NoError(t, os.WriteFile(helpTail, []byte(strings.Repeat("additional help output\n", 65536)), 0o600))
	const helmScript = `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'plugin list') exit 0 ;;
  'plugin install --help')
    printf '%s\n' '--verify verify the plugin before installing it'
    cat "$HELM_HELP_TAIL"
    ;;
  'plugin install --verify=false '*) printf '%s\n' "$*" > "$HELM_CALLS" ;;
  *) exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(directory, "bin", "helm"), []byte(helmScript), 0o700)) //nolint:gosec // The fixture must be executable.
	calls := filepath.Join(directory, "helm-calls")
	command := exec.CommandContext(t.Context(), "make", "-f", "Makefile.gen.app.mk", "install-helm-unittest",
		"SHELL=bash -o pipefail", "HELM_UNITTEST_ATTEMPTS=1")
	command.Dir = directory
	command.Env = append(os.Environ(),
		"PATH="+filepath.Join(directory, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HELM_HELP_TAIL="+helpTail,
		"HELM_CALLS="+calls,
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	installed, err := os.ReadFile(filepath.Clean(calls))
	require.NoError(t, err)
	require.Contains(t, string(installed), "plugin install --verify=false ")
}
