package reservation

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"
)

var (
	// flowFold matches the line break the YAML emitter inserts into a long flow
	// mapping. In flow context a break folds to a single space.
	flowFold = regexp.MustCompile(`\n\s*`)
	// dataKey matches bare and quoted keys inside the ConfigMap data block.
	dataKey = regexp.MustCompile(`^\s+([A-Za-z0-9][A-Za-z0-9.\-_]*|'(?:[^']|'')*'|"(?:[^"\\]|\\.)*")\s*:`)
	// sourceAnnotation matches one reservation annotation line inside a source
	// object's metadata.annotations block, capturing the indentation and the
	// key that follows AnnotationPrefix.
	sourceAnnotation = regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(AnnotationPrefix) + `(\S+):`)
)

// flowMapping renders the fields as a single-line YAML flow mapping, with the
// quoting YAML needs for the values it was given.
func flowMapping(fields [][2]string) (string, error) {
	node := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
	for _, f := range fields {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f[0]},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f[1]},
		)
	}

	out, err := yaml.Marshal(node)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return flowFold.ReplaceAllString(strings.TrimSpace(string(out)), " "), nil
}

// marshalYAML encodes v at the two-space indentation these repos use.
func marshalYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, microerror.Mask(err)
	}
	if err := enc.Close(); err != nil {
		return nil, microerror.Mask(err)
	}

	return buf.Bytes(), nil
}

// setWindowAnnotations rewrites the annotations of the hold in lines, in place,
// and returns how many it rewrote.
func setWindowAnnotations(lines []string, h hold) (found int) {
	want := h.annotations()
	for i, l := range lines {
		m := sourceAnnotation.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if value, ok := want[m[2]]; ok {
			lines[i] = m[1] + AnnotationPrefix + m[2] + ": " + yamlScalar(value)
			found++
		}
	}

	return found
}

// blockEnd returns the index of the last line in the indented block after
// start for which match is true, skipping blank lines and stopping at the
// first line back at start's indentation.
func blockEnd(lines []string, start int, match func(line string) bool) (last int) {
	last = start
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if lines[i] == strings.TrimLeft(lines[i], " \t") {
			break // a line at the outer indentation ends the block
		}
		if match(lines[i]) {
			last = i
		}
	}

	return last
}

// dataBlock locates the ConfigMap `data:` key and the last line that belongs to
// it. It reports false when the file carries no data key.
func dataBlock(lines []string) (start, last int, ok bool) {
	start = -1
	for i, l := range lines {
		if s := strings.TrimSpace(l); s == "data:" || s == "data: {}" {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false
	}

	last = blockEnd(lines, start, dataKey.MatchString)

	return start, last, true
}

// reservationKey compares YAML keys by their decoded identity, so quoted and
// escaped spellings refer to the same reservation as a bare app name.
func reservationKey(line string) (string, bool) {
	m := dataKey.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	var key string
	if err := yaml.Unmarshal([]byte(m[1]), &key); err != nil {
		return "", false
	}
	return key, true
}

// writeReservationLines validates the edited ConfigMap before touching the
// file. In particular, YAML spellings of the same key must never be persisted
// as duplicate reservations that List and the lifecycle commands cannot read.
func writeReservationLines(path string, lines []string) error {
	var configMap struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &configMap); err != nil {
		return microerror.Maskf(invalidConfigError, "parsing edited %s: %v", path, err)
	}
	for app, entry := range configMap.Data {
		if _, err := parseReservationEntry(app, entry); err != nil {
			return microerror.Maskf(invalidConfigError, "parsing edited reservation %q in %s: %v", app, path, err)
		}
	}
	return microerror.Mask(writeLines(path, lines))
}

// findEntry returns the reservation entry app already holds, if any.
func findEntry(lines []string, app string) (string, bool) {
	start, last, ok := dataBlock(lines)
	if !ok {
		return "", false
	}
	for i := start + 1; i <= last; i++ {
		if key, ok := reservationKey(lines[i]); ok && key == app {
			return strings.TrimSpace(lines[i]), true
		}
	}

	return "", false
}

// writeReservationEntry adds app's entry to the ConfigMap data block, or
// replaces it in place, at the same line, when app already holds one.
//
// The caller (checkCollision) is the sole authority on whether the write is
// allowed: an existing entry here is either an expired reservation the reaper
// has not swept yet, freeing the app for a fresh one, or a promotion. Either
// way the line is rewritten, never duplicated, so a promotion never removes
// and re-adds the reservation.
func writeReservationEntry(path, app, entry string) error {
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}

	start, last, ok := dataBlock(lines)
	if !ok {
		return microerror.Maskf(invalidConfigError, "%s holds no `data:` key", path)
	}

	for i := start + 1; i <= last; i++ {
		if key, ok := reservationKey(lines[i]); ok && key == app {
			lines[i] = "  " + entry
			return microerror.Mask(writeReservationLines(path, lines))
		}
	}

	lines[start] = "data:"
	lines = append(lines[:last+1], append([]string{"  " + entry}, lines[last+1:]...)...)

	return microerror.Mask(writeReservationLines(path, lines))
}

// removeReservationEntry removes app's entry from the ConfigMap data block,
// reversing addReservationEntry exactly. When app was the only entry, it
// restores the header line to `data: {}`, exactly as every cluster's
// ConfigMap starts out before its first reservation.
func removeReservationEntry(path, app string) error {
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}

	start, last, ok := dataBlock(lines)
	if !ok {
		return nil // already gone; nothing to undo
	}

	entryIdx := -1
	for i := start + 1; i <= last; i++ {
		if key, ok := reservationKey(lines[i]); ok && key == app {
			entryIdx = i
			break
		}
	}
	if entryIdx < 0 {
		return nil // entry not present; nothing to undo
	}

	remaining := 0
	for i := start + 1; i <= last; i++ {
		if i != entryIdx && dataKey.MatchString(lines[i]) {
			remaining++
		}
	}

	lines = append(lines[:entryIdx], lines[entryIdx+1:]...)
	if remaining == 0 {
		lines[start] = "data: {}"
	}

	return microerror.Mask(writeReservationLines(path, lines))
}

func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the path is built from the repo checkout and the cluster name
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return strings.Split(string(b), "\n"), nil
}

func writeLines(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644) //nolint:gosec // a file committed to a GitOps repo
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// yamlScalar renders s as the YAML encoder that reserve uses would, quoted only
// where YAML needs it, so an extended file reads like a freshly reserved one.
func yamlScalar(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		return strconv.Quote(s) // a string always marshals; keep a valid scalar anyway
	}

	return strings.TrimSuffix(string(out), "\n")
}
