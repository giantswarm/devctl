package reposetup

import (
	"bytes"
	"strings"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"
)

// Creation is what a person declares to create a repository: the fields of
// `devctl repo create`, the same the Repositories page and
// giantswarm-repo-manager's create_repository take. Everything else the
// entry needs is a default the validation applies -- and the opt-in to
// alignment, which a creation always carries: a repository created through
// the product is opted in by its creation, so the reconciler sets it up
// from the merged entry instead of only checking it.
type Creation struct {
	Name          string
	ComponentType string
	Description   string
	Visibility    string
	Flavours      []string
	Language      string
}

// Declaration renders the creation as a team-file entry in the key order the
// team files use: name, description, visibility, componentType, align, gen.
// Empty fields are left out, so the validation names what is missing.
// align is always true: the creation is the repository's opt-in to
// alignment, and the reconciler reads it from the file on every trigger.
// gen.ci.generate is written out -- align-files reads the file, not the dry
// run -- as the CircleCI generator decides: true when it has a job for the
// declaration (a Go or Node build, a chart from the app flavour), false when
// it would generate an empty pipeline, which the validation refuses.
func (c Creation) Declaration() (Declaration, error) {
	if c.Name == "" {
		return Declaration{}, microerror.Maskf(invalidConfigError, "%T.Name must not be empty", c)
	}

	node := mappingNode()
	setMappingValue(node, "name", scalarNode(c.Name))
	if c.Description != "" {
		setMappingValue(node, "description", scalarNode(c.Description))
	}
	if c.Visibility != "" {
		setMappingValue(node, "visibility", scalarNode(c.Visibility))
	}
	if c.ComponentType != "" {
		setMappingValue(node, "componentType", scalarNode(c.ComponentType))
	}
	setMappingValue(node, "align", boolNode(true))

	gen := mappingNode()
	if len(c.Flavours) > 0 {
		flavours := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, f := range c.Flavours {
			flavours.Content = append(flavours.Content, scalarNode(f))
		}
		setMappingValue(gen, "flavours", flavours)
	}
	if c.Language != "" {
		setMappingValue(gen, "language", scalarNode(c.Language))
	}
	ci := mappingNode()
	generate := hasCIJob(Fields{Gen: &GenFields{Flavours: c.Flavours, Language: c.Language}})
	setMappingValue(ci, "generate", boolNode(generate))
	setMappingValue(gen, "ci", ci)
	setMappingValue(node, "gen", gen)

	return Declaration{Name: c.Name, node: node}, nil
}

// InsertEntry returns the team file with d added at its alphabetical place:
// after the last entry whose name sorts before d's (case-insensitively),
// before the first entry when none does, at the end of a file without
// entries. The file's own text is kept byte for byte -- header comment, key
// order and comments of the other entries -- so the pull request's diff is
// the new entry and nothing else. A comment block at column 0 right above
// the next entry is that entry's and stays with it.
func InsertEntry(team string, file []byte, d Declaration) ([]byte, error) {
	tf, err := ParseTeamFile(team, bytes.NewReader(file))
	if err != nil {
		return nil, microerror.Mask(err)
	}
	rendered, err := d.YAML()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	lines := bytes.SplitAfter(file, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1] // the split after a final newline
	}
	var starts []int
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("- ")) {
			starts = append(starts, i)
		}
	}
	if len(starts) != len(tf.Entries) {
		return nil, microerror.Maskf(invalidTeamFileError, "team file of %s: %d entries but %d list items start at column 0; cannot place the new entry", team, len(tf.Entries), len(starts))
	}

	after := -1
	for i, e := range tf.Entries {
		if strings.ToLower(e.Name) < strings.ToLower(d.Name) {
			after = i
		}
	}

	at := len(lines)
	switch {
	case len(starts) == 0 || after == len(starts)-1:
		// at the end
	case after < 0:
		at = starts[0]
	default:
		at = starts[after+1]
		for at-1 > starts[after] && bytes.HasPrefix(lines[at-1], []byte("#")) {
			at--
		}
	}

	var out bytes.Buffer
	for i, line := range lines {
		if i == at {
			out.WriteString(rendered)
		}
		out.Write(line)
		if i == len(lines)-1 && !bytes.HasSuffix(line, []byte("\n")) {
			out.WriteByte('\n')
		}
	}
	if at == len(lines) {
		out.WriteString(rendered)
	}

	return out.Bytes(), nil
}
