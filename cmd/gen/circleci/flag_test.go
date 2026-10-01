package circleci

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// embeddedSchemaPath is the repositories schema devctl embeds, the copy
// giantswarm-repo-manager validates every team-file entry against.
const embeddedSchemaPath = "../../../pkg/reposetup/schema/repositories.schema.json"

// notGenCIKeys are the flags no gen.ci key sets: the entry supplies them
// elsewhere, the command detects them from the repository, or they are
// deprecated.
var notGenCIKeys = map[string]string{
	flagRepoName:            "the entry's name",
	flagTeam:                "the team file",
	flagComponentType:       "the entry's componentType",
	flagFlavour:             "gen.flavours",
	flagLanguage:            "gen.language",
	flagGoBuildPath:         "the orb default",
	flagPackageManager:      "detected from the lockfile",
	flagNodeImageVersion:    "detected from .nvmrc",
	flagKeepChartAppVersion: "deprecated",
}

// Every flag of `devctl gen circleci` that a team file sets is a gen.ci key
// of the embedded schema, whose description names the flag it is passed to
// (--<flag>).
// A flag without its key passes the reconciler, which reads giantswarm/github's
// schema, and is refused as "not a field of the repositories schema" by the
// manager, which reads the embedded copy: the entry then gets no checks at all.
func TestFlagsAreGenCIKeysOfTheEmbeddedSchema(t *testing.T) {
	doc, err := os.ReadFile(embeddedSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Items struct {
			Properties struct {
				Gen struct {
					Properties struct {
						CI json.RawMessage `json:"ci"`
					} `json:"properties"`
				} `json:"gen"`
			} `json:"properties"`
		} `json:"items"`
	}
	if err := json.Unmarshal(doc, &schema); err != nil {
		t.Fatal(err)
	}
	var ci any
	if err := json.Unmarshal(schema.Items.Properties.Gen.Properties.CI, &ci); err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	passedTo := regexp.MustCompile(`(?:^|[^a-z0-9-])--([a-z0-9]+(?:-[a-z0-9]+)*)`)
	for _, d := range descriptions(ci) {
		for _, m := range passedTo.FindAllStringSubmatch(d, -1) {
			declared[m[1]] = true
		}
	}

	cmd := &cobra.Command{}
	(&flag{}).Init(cmd)
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if _, ok := notGenCIKeys[f.Name]; ok {
			if declared[f.Name] {
				t.Errorf("--%s is a gen.ci key of %s: drop it from notGenCIKeys", f.Name, embeddedSchemaPath)
			}
			return
		}
		if !declared[f.Name] {
			t.Errorf("--%s has no gen.ci key in %s: add the key giantswarm/github's schema declares for it, its description naming --%s", f.Name, embeddedSchemaPath, f.Name)
		}
	})
}

// descriptions returns every description in a schema subtree.
func descriptions(node any) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if s, ok := v.(string); ok && k == "description" {
				out = append(out, s)
				continue
			}
			out = append(out, descriptions(v)...)
		}
	case []any:
		for _, v := range n {
			out = append(out, descriptions(v)...)
		}
	}
	return out
}
