package renovate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/titanous/json5"
)

// TestSetReviewers covers the surgical edit with hand-crafted inputs and exact
// expected output, so both the "replace existing value" and "insert new key"
// paths are pinned down byte-for-byte.
func TestSetReviewers(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		reviewers   []string
		singleQuote bool
		expected    string
	}{
		{
			name:        "insert into empty object (json5, single quotes)",
			input:       "{}",
			reviewers:   []string{"team:team-rocket"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['team:team-rocket']\n}",
		},
		{
			name:        "insert into empty object (json, double quotes, no trailing comma)",
			input:       "{}",
			reviewers:   []string{"team:team-rocket"},
			singleQuote: false,
			expected:    "{\n  \"reviewers\": [\"team:team-rocket\"]\n}",
		},
		{
			name:        "insert into empty object with comment only",
			input:       "{\n  // nothing yet\n}",
			reviewers:   []string{"r"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['r']\n\n  // nothing yet\n}",
		},
		{
			name:        "insert before existing keys preserves them",
			input:       "{\n  extends: ['foo'],\n}",
			reviewers:   []string{"team:team-rocket"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['team:team-rocket'],\n  extends: ['foo'],\n}",
		},
		{
			name:        "replace existing single-quoted reviewers",
			input:       "{\n  reviewers: ['old'],\n  extends: ['foo'],\n}",
			reviewers:   []string{"team:team-rocket", "team:team-honeybadger"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['team:team-rocket', 'team:team-honeybadger'],\n  extends: ['foo'],\n}",
		},
		{
			name:        "replace existing double-quoted reviewers",
			input:       "{\n  \"reviewers\": [\"old\"],\n  \"extends\": [\"foo\"]\n}",
			reviewers:   []string{"alice"},
			singleQuote: false,
			expected:    "{\n  \"reviewers\": [\"alice\"],\n  \"extends\": [\"foo\"]\n}",
		},
		{
			name:        "replace multi-line reviewers array",
			input:       "{\n  reviewers: [\n    'a',\n    'b',\n  ],\n  prConcurrentLimit: 5,\n}",
			reviewers:   []string{"team:team-rocket"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['team:team-rocket'],\n  prConcurrentLimit: 5,\n}",
		},
		{
			name:        "reviewersFromCodeOwners is not mistaken for reviewers",
			input:       "{\n  reviewersFromCodeOwners: false,\n}",
			reviewers:   []string{"team:team-rocket"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['team:team-rocket'],\n  reviewersFromCodeOwners: false,\n}",
		},
		{
			name:        "nested additionalReviewers is not mistaken for top-level reviewers",
			input:       "{\n  packageRules: [\n    {\n      additionalReviewers: ['team:team-phoenix'],\n    },\n  ],\n}",
			reviewers:   []string{"team:team-rocket"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['team:team-rocket'],\n  packageRules: [\n    {\n      additionalReviewers: ['team:team-phoenix'],\n    },\n  ],\n}",
		},
		{
			name:        "nested reviewers inside a packageRule is not touched",
			input:       "{\n  packageRules: [\n    {\n      reviewers: ['nested'],\n    },\n  ],\n}",
			reviewers:   []string{"top"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['top'],\n  packageRules: [\n    {\n      reviewers: ['nested'],\n    },\n  ],\n}",
		},
		{
			name:        "comment between key and value is preserved on replace",
			input:       "{\n  reviewers: /* keep */ ['old'],\n}",
			reviewers:   []string{"new"},
			singleQuote: true,
			expected:    "{\n  reviewers: /* keep */ ['new'],\n}",
		},
		{
			name:        "leading line comments before root object are preserved on insert",
			input:       "// header\n{\n  extends: ['foo'],\n}",
			reviewers:   []string{"r"},
			singleQuote: true,
			expected:    "// header\n{\n  reviewers: ['r'],\n  extends: ['foo'],\n}",
		},
		{
			name:        "string value containing braces does not confuse replace",
			input:       "{\n  reviewers: ['a'],\n  commitMessageTopic: '{{parentDir}}',\n}",
			reviewers:   []string{"b"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['b'],\n  commitMessageTopic: '{{parentDir}}',\n}",
		},
		{
			name:        "value with quote is escaped",
			input:       "{}",
			reviewers:   []string{"a'b"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['a\\'b']\n}",
		},
		{
			// .json5 default, but the file uses double quotes (e.g. happa): the
			// rendered value must follow the file, not the extension default.
			name:        "double-quoted json5 file gets double-quoted reviewers",
			input:       "{\n  extends: [\"foo\"],\n}",
			reviewers:   []string{"r"},
			singleQuote: true,
			expected:    "{\n  reviewers: [\"r\"],\n  extends: [\"foo\"],\n}",
		},
		{
			// Inverse: a single-quoted file even with a .json default.
			name:        "single-quoted file gets single-quoted reviewers",
			input:       "{\n  extends: ['foo'],\n}",
			reviewers:   []string{"r"},
			singleQuote: false,
			expected:    "{\n  reviewers: ['r'],\n  extends: ['foo'],\n}",
		},
		{
			name:        "mixed quotes pick the dominant style (single wins)",
			input:       "{\n  a: ['x', 'y'],\n  b: [\"z\"],\n}",
			reviewers:   []string{"r"},
			singleQuote: false,
			expected:    "{\n  reviewers: ['r'],\n  a: ['x', 'y'],\n  b: [\"z\"],\n}",
		},
		{
			name:        "quoted keys are matched on insert",
			input:       "{\n  \"extends\": [\"foo\"],\n}",
			reviewers:   []string{"r"},
			singleQuote: true,
			expected:    "{\n  \"reviewers\": [\"r\"],\n  \"extends\": [\"foo\"],\n}",
		},
		{
			name:        "double quotes inside single-quoted strings are not counted",
			input:       "{\n  matchStrings: ['version = \"(?<v>[^\"]+)\"'],\n}",
			reviewers:   []string{"r"},
			singleQuote: false,
			expected:    "{\n  reviewers: ['r'],\n  matchStrings: ['version = \"(?<v>[^\"]+)\"'],\n}",
		},
		{
			// Single-quote house style with a customManagers block whose regex
			// matchStrings are double-quoted. A flat quote count would be
			// dominated by the nested double quotes; the shallow count must
			// keep the file single-quoted.
			name:        "nested double-quoted regex does not outvote shallow single quotes",
			input:       "{\n  extends: ['foo'],\n  customManagers: [\n    { matchStrings: [\"a\", \"b\", \"c\", \"d\"] },\n  ],\n}",
			reviewers:   []string{"r"},
			singleQuote: true,
			expected:    "{\n  reviewers: ['r'],\n  extends: ['foo'],\n  customManagers: [\n    { matchStrings: [\"a\", \"b\", \"c\", \"d\"] },\n  ],\n}",
		},
		{
			// Double-quoted keys with single-quoted values: the inserted key
			// must use the key style (double), the value the value style.
			name:        "key quote follows first key, value quote follows values",
			input:       "{\n  \"extends\": ['foo'],\n  \"x\": ['a', 'b'],\n}",
			reviewers:   []string{"r"},
			singleQuote: true,
			expected:    "{\n  \"reviewers\": ['r'],\n  \"extends\": ['foo'],\n  \"x\": ['a', 'b'],\n}",
		},
		{
			name:        "tab indentation is reused",
			input:       "{\n\textends: ['foo'],\n}",
			reviewers:   []string{"r"},
			singleQuote: true,
			expected:    "{\n\treviewers: ['r'],\n\textends: ['foo'],\n}",
		},
		{
			name:        "four-space indentation is reused",
			input:       "{\n    extends: ['foo'],\n}",
			reviewers:   []string{"r"},
			singleQuote: true,
			expected:    "{\n    reviewers: ['r'],\n    extends: ['foo'],\n}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := setReviewers([]byte(tc.input), tc.reviewers, tc.singleQuote)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(out) != tc.expected {
				t.Errorf("unexpected output\n got: %q\nwant: %q", string(out), tc.expected)
			}
		})
	}
}

func TestSetReviewersErrors(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{name: "not an object", input: "[1, 2, 3]"},
		{name: "empty input", input: ""},
		{name: "only whitespace and comments", input: "// nothing here\n"},
		{name: "unterminated object", input: "{ extends: ['a'] "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := setReviewers([]byte(tc.input), []string{"r"}, true)
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
		})
	}
}

// TestSetReviewersWild runs the surgical edit over a corpus of real Renovate
// configs harvested from the giantswarm org and independently verifies the
// result with a third-party JSON5 parser. This guards against the scanner
// corrupting any real-world file.
func TestSetReviewersWild(t *testing.T) {
	files, err := filepath.Glob("testdata/wild/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no wild fixtures found in testdata/wild")
	}

	reviewers := []string{"team:team-honeybadger", "team:team-rocket"}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			// The fixture must itself be valid JSON5, otherwise the test is
			// meaningless.
			var origMap map[string]any
			if err := json5.Unmarshal(src, &origMap); err != nil {
				t.Skipf("fixture is not parseable by the reference JSON5 parser: %v", err)
			}

			singleQuote := strings.HasSuffix(file, ".json5")

			out, err := setReviewers(src, reviewers, singleQuote)
			if err != nil {
				t.Fatalf("setReviewers failed: %v", err)
			}

			// 1. The edited file must still be valid JSON5.
			var modMap map[string]any
			if err := json5.Unmarshal(out, &modMap); err != nil {
				t.Fatalf("edited config no longer parses as JSON5: %v\n---\n%s", err, string(out))
			}

			// 2. The reviewers value must be exactly what we set.
			gotReviewers := toStringSlice(t, modMap["reviewers"])
			if !reflect.DeepEqual(gotReviewers, reviewers) {
				t.Errorf("reviewers = %v, want %v", gotReviewers, reviewers)
			}

			// 3. Every other top-level key must be untouched.
			delete(origMap, "reviewers")
			delete(modMap, "reviewers")
			if !reflect.DeepEqual(origMap, modMap) {
				t.Errorf("non-reviewers content changed\n orig keys: %v\n mod  keys: %v", sortedKeys(origMap), sortedKeys(modMap))
			}

			// 4. Every comment in the original must survive verbatim.
			for line := range strings.SplitSeq(string(src), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") {
					if !strings.Contains(string(out), trimmed) {
						t.Errorf("comment lost after edit: %q", trimmed)
					}
				}
			}

			// 5. The reviewers must be rendered with the same quote character
			// the file uses for its `extends` array. This is an independent
			// oracle for the quote-detection logic: every config extends a
			// preset, written in the file's own convention.
			if q := quoteOfExtends(src); q != 0 {
				want := string(q) + reviewers[0] + string(q)
				if !strings.Contains(string(out), want) {
					t.Errorf("reviewers not rendered with the file's quote style (%q); output:\n%s", string(q), string(out))
				}
			}
		})
	}
}

// quoteOfExtends returns the quote character (' or ") that the file uses around
// the first string near its `extends` key, or 0 if it cannot be determined.
func quoteOfExtends(src []byte) byte {
	idx := bytes.Index(src, []byte("extends"))
	if idx < 0 {
		return 0
	}
	for i := idx; i < len(src); i++ {
		if src[i] == '\'' || src[i] == '"' {
			return src[i]
		}
	}
	return 0
}

func TestFindConfigFile(t *testing.T) {
	t.Run("prefers json5 over json", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "renovate.json"), "{}")
		writeFile(t, filepath.Join(dir, "renovate.json5"), "{}")

		got, err := FindConfigFile(dir)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "renovate.json5" {
			t.Errorf("got %q, want renovate.json5", got)
		}
	})

	t.Run("falls back to json", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "renovate.json"), "{}")

		got, err := FindConfigFile(dir)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "renovate.json" {
			t.Errorf("got %q, want renovate.json", got)
		}
	})

	t.Run("errors when no config exists", func(t *testing.T) {
		dir := t.TempDir()
		_, err := FindConfigFile(dir)
		if !IsConfigNotFound(err) {
			t.Errorf("expected configNotFoundError, got %v", err)
		}
	})
}

// TestSetReviewersRoundTrip exercises the full public SetReviewers including
// the filesystem read/write and quote-style selection by extension.
func TestSetReviewersRoundTrip(t *testing.T) {
	t.Run("json5 uses single quotes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "renovate.json5")
		writeFile(t, path, "{\n  extends: ['foo'],\n}")

		if err := SetReviewers(path, []string{"team:team-rocket"}); err != nil {
			t.Fatal(err)
		}

		got := readFile(t, path)
		if !strings.Contains(got, "reviewers: ['team:team-rocket']") {
			t.Errorf("expected single-quoted reviewers, got:\n%s", got)
		}
	})

	t.Run("json uses double quotes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "renovate.json")
		writeFile(t, path, "{\n  \"extends\": [\"foo\"]\n}")

		if err := SetReviewers(path, []string{"alice"}); err != nil {
			t.Fatal(err)
		}

		got := readFile(t, path)
		if !strings.Contains(got, "\"reviewers\": [\"alice\"]") {
			t.Errorf("expected double-quoted reviewers, got:\n%s", got)
		}
	})

	t.Run("file mode is preserved", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "renovate.json5")
		writeFile(t, path, "{\n  extends: ['foo'],\n}")
		if err := os.Chmod(path, 0640); err != nil {
			t.Fatal(err)
		}

		if err := SetReviewers(path, []string{"r"}); err != nil {
			t.Fatal(err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0640 {
			t.Errorf("mode = %o, want 640", got)
		}
	})

	// Inserting into an empty renovate.json must stay valid *strict* JSON: the
	// stdlib parser rejects the trailing comma a naive insertion would leave.
	t.Run("empty json stays strict-JSON valid", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "renovate.json")
		writeFile(t, path, "{}")

		if err := SetReviewers(path, []string{"alice"}); err != nil {
			t.Fatal(err)
		}

		got := readFile(t, path)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(got), &parsed); err != nil {
			t.Fatalf("result is not valid strict JSON: %v\n%s", err, got)
		}
	})
}

func toStringSlice(t *testing.T, v any) []string {
	t.Helper()
	raw, ok := v.([]any)
	if !ok {
		t.Fatalf("reviewers is not an array: %T", v)
	}
	out := make([]string, len(raw))
	for i, e := range raw {
		s, ok := e.(string)
		if !ok {
			t.Fatalf("reviewer entry is not a string: %T", e)
		}
		out[i] = s
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
