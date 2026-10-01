package reposetup

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// embeddedEnum reads a field's enum from the embedded JSON by walking the
// document itself, independently of the compiled schema.
func embeddedEnum(t *testing.T, path ...string) []string {
	t.Helper()

	var doc map[string]any
	require.NoError(t, json.Unmarshal(embeddedSchema, &doc))

	node := doc["items"].(map[string]any)
	for _, name := range path {
		node = node["properties"].(map[string]any)[name].(map[string]any)
	}
	if items, ok := node["items"].(map[string]any); ok {
		node = items
	}

	var values []string
	for _, v := range node["enum"].([]any) {
		values = append(values, v.(string))
	}
	require.NotEmpty(t, values)
	return values
}

func TestSchemaFieldValues(t *testing.T) {
	schema, err := EmbeddedSchema()
	require.NoError(t, err)

	testCases := []struct {
		path string
		json []string
	}{
		{path: "componentType", json: []string{"componentType"}},
		{path: "gen.flavours", json: []string{"gen", "flavours"}},
		{path: "gen.language", json: []string{"gen", "language"}},
		{path: "visibility", json: []string{"visibility"}},
		{path: "lifecycle", json: []string{"lifecycle"}},
	}
	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			got, err := schema.FieldValues(tc.path)
			require.NoError(t, err)
			require.Equal(t, embeddedEnum(t, tc.json...), got)
			require.Equal(t, got, EmbeddedFieldValues(tc.path))
		})
	}
}

func TestSchemaFieldValuesErrors(t *testing.T) {
	schema, err := EmbeddedSchema()
	require.NoError(t, err)

	testCases := []struct {
		name string
		path string
	}{
		{name: "unknown field", path: "colour"},
		{name: "unknown nested field", path: "gen.colour"},
		{name: "path below an array", path: "gen.flavours.name"},
		{name: "path below a scalar", path: "visibility.name"},
		{name: "empty path", path: ""},
		{name: "field without an enum", path: "name"},
		{name: "object without an enum", path: "gen"},
		{name: "array without an item enum", path: "requiredChecks"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			values, err := schema.FieldValues(tc.path)
			require.True(t, IsInvalidConfig(err), "want an invalid config error, got %v", err)
			require.Nil(t, values)
			require.Nil(t, EmbeddedFieldValues(tc.path))
		})
	}
}

// A schema answers from its own document, following local $refs, not from
// the embedded copy.
func TestSchemaFieldValuesFollowsRefs(t *testing.T) {
	doc := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "array",
		"items": {"$ref": "#/$defs/entry"},
		"$defs": {
			"entry": {
				"type": "object",
				"properties": {
					"componentType": {"$ref": "#/$defs/componentType"},
					"gen": {
						"type": "object",
						"properties": {
							"flavours": {"type": "array", "items": {"$ref": "#/$defs/flavour"}}
						}
					}
				}
			},
			"componentType": {"type": "string", "enum": ["service", "cli"]},
			"flavour": {"type": "string", "enum": ["zeta", "alpha"]}
		}
	}`)
	schema, err := CompileSchema(doc, SchemaOriginFile)
	require.NoError(t, err)

	got, err := schema.FieldValues("componentType")
	require.NoError(t, err)
	require.Equal(t, []string{"service", "cli"}, got)

	got, err = schema.FieldValues("gen.flavours")
	require.NoError(t, err)
	require.Equal(t, []string{"zeta", "alpha"}, got)

	_, err = schema.FieldValues("visibility")
	require.True(t, IsInvalidConfig(err), "want an invalid config error, got %v", err)
}

func TestSchemaFieldValuesNonStringEnum(t *testing.T) {
	doc := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "array",
		"items": {"type": "object", "properties": {"size": {"enum": ["small", 2]}}}
	}`)
	schema, err := CompileSchema(doc, SchemaOriginFile)
	require.NoError(t, err)

	values, err := schema.FieldValues("size")
	require.True(t, IsInvalidSchema(err), "want an invalid schema error, got %v", err)
	require.Nil(t, values)
}
