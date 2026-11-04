package reposetup

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// The embedded schema admits as gen.flavours exactly the flavours devctl's
// generators accept. A value the schema admits but gen.NewFlavour refuses
// passes the validation and fails every `devctl gen` run for the repository.
func TestEmbeddedSchemaFlavoursAreTheGenerators(t *testing.T) {
	var schema struct {
		Items struct {
			Properties struct {
				Gen struct {
					Properties struct {
						Flavours struct {
							Items struct {
								Enum []string `json:"enum"`
							} `json:"items"`
						} `json:"flavours"`
					} `json:"properties"`
				} `json:"gen"`
			} `json:"properties"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(embeddedSchema, &schema))

	got := schema.Items.Properties.Gen.Properties.Flavours.Items.Enum
	want := gen.AllFlavours()
	sort.Strings(got)
	sort.Strings(want)
	require.Equal(t, want, got, "gen.flavours in schema/repositories.schema.json and gen.AllFlavours() drifted apart")
}

// The embedded schema admits as gen.language exactly the languages devctl's
// generators accept.
func TestEmbeddedSchemaLanguagesAreTheGenerators(t *testing.T) {
	schema, err := EmbeddedSchema()
	require.NoError(t, err)

	got, err := schema.FieldValues("gen.language")
	require.NoError(t, err)
	want := gen.AllLanguages()
	sort.Strings(got)
	sort.Strings(want)
	require.Equal(t, want, got, "gen.language in schema/repositories.schema.json and gen.AllLanguages() drifted apart")
}

func TestEmbeddedSchemaProtection(t *testing.T) {
	schema, err := EmbeddedSchema()
	require.NoError(t, err)

	testCases := []struct {
		name     string
		value    any
		declared bool
		wantOK   bool
	}{
		{name: "case 0: omitted keeps the flavour's default", wantOK: true},
		{name: "case 1: baseline opts in", value: "baseline", declared: true, wantOK: true},
		{name: "case 2: an unknown policy is refused", value: "basline", declared: true},
		{name: "case 3: an empty policy is refused", value: "", declared: true},
		{name: "case 4: a boolean is refused", value: true, declared: true},
		{name: "case 5: null is refused", declared: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			entry := map[string]any{
				"name":          "customer-gitops",
				"componentType": "customer",
				"gen":           map[string]any{"flavours": []any{"customer"}, "language": "generic"},
			}
			if tc.declared {
				entry["protection"] = tc.value
			}
			problems := schema.Problems(entry)
			if tc.wantOK {
				require.Empty(t, problems)
				return
			}
			require.NotEmpty(t, problems)
			for _, problem := range problems {
				require.Equal(t, "protection", problem.Field)
			}
		})
	}
}
