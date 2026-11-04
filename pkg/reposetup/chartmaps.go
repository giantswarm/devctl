package reposetup

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// openChartValueMaps annotates unannotated empty maps from the chart template
// before schema generation closes objects by default. The annotation becomes
// part of the new chart's values.yaml, so later schema generation preserves it.
// Only scaffold creation calls this; existing repositories keep their schemas.
func openChartValueMaps(dir string) error {
	paths, err := filepath.Glob(filepath.Join(dir, "helm", "*", "values.yaml"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path) // #nosec G304 -- path is under the newly created scaffold
		if err != nil {
			return err
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("annotating empty maps in %s: %w", path, err)
		}
		if !annotateEmptyValueMaps(&doc) {
			continue
		}
		var out bytes.Buffer
		encoder := yaml.NewEncoder(&out)
		encoder.SetIndent(2)
		if err := encoder.Encode(&doc); err != nil {
			return err
		}
		if err := encoder.Close(); err != nil {
			return err
		}
		if err := writeFile(path, out.Bytes(), fileMode); err != nil {
			return err
		}
	}
	return nil
}

func annotateEmptyValueMaps(node *yaml.Node) bool {
	changed := false
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if value.Kind != yaml.MappingNode || len(value.Content) != 0 {
				continue
			}
			// Any existing schema annotation belongs to the template author,
			// including $ref, typed maps and explicitly closed empty objects.
			comments := strings.Join([]string{key.HeadComment, key.LineComment, key.FootComment, value.LineComment}, "\n")
			annotated := false
			for line := range strings.SplitSeq(comments, "\n") {
				line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
				if strings.HasPrefix(line, "@schema") {
					annotated = true
					break
				}
			}
			if !annotated {
				key.HeadComment = strings.TrimPrefix(key.HeadComment+"\n# @schema additionalProperties: true", "\n")
				changed = true
			}
		}
	}
	for _, child := range node.Content {
		if annotateEmptyValueMaps(child) {
			changed = true
		}
	}
	return changed
}
