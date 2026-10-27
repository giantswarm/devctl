// Package golangci generates the golangci-lint configuration of a Go
// repository, so that a bare `golangci-lint run`, the generated `make lint`
// and the generated pre-commit hook all lint with the same linters.
package golangci

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

// ConfigFile is the configuration written to a repository that has none.
const ConfigFile = ".golangci.yaml"

// configFiles are the names golangci-lint reads its configuration from, in
// the order it looks for them.
var configFiles = []string{".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json"}

// Linters are the linters a generated Go repository lints with on top of
// golangci-lint's standard set.
var Linters = []string{"gosec", "goconst"}

// govet is in golangci-lint's standard set, so it is only enabled explicitly
// where a configuration replaces that set.
const govet = "govet"

const defaultConfig = `# The repository owns this file: devctl creates it when it is missing and
# afterwards only adds the linters it requires (gosec, goconst) to
# linters.enable when they are neither enabled nor disabled.
version: "2"

linters:
  enable:
    - gosec
    - goconst

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
`

// NewInput returns the input that writes the configuration of the repository
// in dir: ConfigFile when the repository has no configuration, otherwise its
// own configuration with the missing Linters added to linters.enable and
// nothing else changed.
func NewInput(dir string) input.Input {
	path := filepath.Join(dir, ConfigFile)
	existing := ""
	for _, name := range configFiles {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			path, existing = p, p
			break
		}
	}

	return input.Input{
		Path:           path,
		SkipRegenCheck: true,
		Generate: func(context.Context) ([]byte, error) {
			if existing == "" {
				return []byte(defaultConfig), nil
			}
			content, err := os.ReadFile(existing) // #nosec G304 -- one of the fixed configuration names in the repository
			if err != nil {
				return nil, microerror.Mask(err)
			}
			return Ensure(existing, content)
		},
	}
}

// Ensure returns content, the configuration read from path, with every linter
// of Linters added to linters.enable that the configuration does not already
// enable, disable or get from `linters.default: all`. Lines are inserted into
// content as they are, so comments and layout stay; a configuration that
// needs nothing is returned unchanged.
func Ensure(path string, content []byte) ([]byte, error) {
	if ext := filepath.Ext(path); ext != ".yml" && ext != ".yaml" {
		return nil, microerror.Maskf(unsupportedConfigError, "%s: devctl edits only YAML configurations; enable %s in linters.enable", path, strings.Join(Linters, ", "))
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, microerror.Maskf(unsupportedConfigError, "%s: %v", path, err)
	}

	lines := strings.SplitAfter(string(content), "\n")
	if len(doc.Content) == 0 {
		return []byte(appendLines(lines, enableBlock("", missing("", nil, nil)))), nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode || root.Style&yaml.FlowStyle != 0 {
		return nil, unsupported(path, "the document is not a block mapping")
	}

	lintersKey, linters := lookup(root, "linters")
	if linters == nil {
		return []byte(appendLines(lines, enableBlock("", missing("", nil, nil)))), nil
	}
	if isNull(linters) {
		return []byte(insertAfter(lines, lintersKey.Line, enableBlock("  ", missing("", nil, nil)))), nil
	}
	if linters.Kind != yaml.MappingNode || linters.Style&yaml.FlowStyle != 0 {
		return nil, unsupported(path, "linters is not a block mapping")
	}

	def := ""
	if _, n := lookup(linters, "default"); n != nil {
		def = n.Value
	}
	_, disable := lookup(linters, "disable")
	enableKey, enable := lookup(linters, "enable")

	add := missing(def, scalars(enable), scalars(disable))
	if len(add) == 0 {
		return content, nil
	}

	indent := strings.Repeat(" ", linters.Content[0].Column-1)
	switch {
	case enable == nil:
		return []byte(insertAfter(lines, lintersKey.Line, enableBlock(indent, add))), nil
	case isNull(enable):
		return []byte(insertAfter(lines, enableKey.Line, enableItems(indent+"  ", add))), nil
	case enable.Kind != yaml.SequenceNode || enable.Style&yaml.FlowStyle != 0 || len(enable.Content) == 0:
		return nil, unsupported(path, "linters.enable is not a block sequence")
	}

	last := enable.Content[len(enable.Content)-1]
	if last.Kind != yaml.ScalarNode || strings.Contains(last.Value, "\n") {
		return nil, unsupported(path, "linters.enable holds something other than linter names")
	}
	first := lines[enable.Content[0].Line-1]
	itemIndent := first[:len(first)-len(strings.TrimLeft(first, " "))]
	return []byte(insertAfter(lines, last.Line, enableItems(itemIndent, add))), nil
}

// missing returns the linters to add to a configuration whose linters.default
// is def and which enables enabled and disables disabled.
func missing(def string, enabled, disabled []string) []string {
	if def == "all" {
		return nil
	}
	want := slices.Clone(Linters)
	if def != "" && def != "standard" {
		want = append(want, govet)
	}
	var add []string
	for _, l := range want {
		if !slices.Contains(enabled, l) && !slices.Contains(disabled, l) {
			add = append(add, l)
		}
	}
	return add
}

func enableBlock(indent string, linters []string) string {
	if indent == "" {
		return "linters:\n" + enableBlock("  ", linters)
	}
	return indent + "enable:\n" + enableItems(indent+"  ", linters)
}

func enableItems(indent string, linters []string) string {
	var b strings.Builder
	for _, l := range linters {
		fmt.Fprintf(&b, "%s- %s\n", indent, l)
	}
	return b.String()
}

// insertAfter inserts text after the 1-based line of lines.
func insertAfter(lines []string, line int, text string) string {
	var b bytes.Buffer
	for i, l := range lines {
		b.WriteString(l)
		if i == line-1 {
			if !strings.HasSuffix(l, "\n") {
				b.WriteString("\n")
			}
			b.WriteString(text)
		}
	}
	return b.String()
}

func appendLines(lines []string, text string) string {
	content := strings.Join(lines, "")
	if strings.TrimSpace(content) == "" {
		return text
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + "\n" + text
}

func lookup(mapping *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}
	return nil, nil
}

func scalars(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, c := range n.Content {
		if c.Kind == yaml.ScalarNode {
			out = append(out, c.Value)
		}
	}
	return out
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

func unsupported(path, reason string) error {
	return microerror.Maskf(unsupportedConfigError, "%s: %s; enable %s in linters.enable", path, reason, strings.Join(Linters, ", "))
}
