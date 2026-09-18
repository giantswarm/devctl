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
