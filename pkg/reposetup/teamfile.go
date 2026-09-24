package reposetup

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
	sigsyaml "sigs.k8s.io/yaml"
)

// TeamFile is one repositories/<team>.yaml of giantswarm/github: the team
// that owns every entry in it and the entries themselves.
type TeamFile struct {
	// Team is the GitHub team slug the file belongs to: the file's base name
	// without extension (repositories/team-bumblebee.yaml → team-bumblebee).
	Team string
	// Path is where the file was read from; empty for a parsed reader.
	Path string
	// Entries in file order.
	Entries []Declaration
}

// Declaration is one entry of a team file: the desired state of one
// repository. It keeps the YAML node it was read from, so a rendered entry
// preserves the author's key order and comments.
type Declaration struct {
	// Name is the entry's name field; empty when the entry has none.
	Name string
	node *yaml.Node
}

// TeamOf returns the team slug a team-file path stands for: the base name
// without extension.
func TeamOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// ReadTeamFile reads a team file; the team is [TeamOf] the path.
func ReadTeamFile(path string) (*TeamFile, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, microerror.Mask(err)
	}
	defer func() { _ = f.Close() }()

	tf, err := ParseTeamFile(TeamOf(path), f)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	tf.Path = path

	return tf, nil
}

// ParseTeamFile parses a team file for the named team. The file is a YAML
// list of mappings; an empty file is a team with no entries.
func ParseTeamFile(team string, r io.Reader) (*TeamFile, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, microerror.Maskf(invalidTeamFileError, "team file of %s: %v", team, err)
	}

	tf := &TeamFile{Team: team}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return tf, nil
	}

	list := doc.Content[0]
	if list.Kind != yaml.SequenceNode {
		return nil, microerror.Maskf(invalidTeamFileError, "team file of %s: expected a list of entries at the top level, got a %s", team, kindName(list.Kind))
	}

	for i, item := range list.Content {
		if item.Kind != yaml.MappingNode {
			return nil, microerror.Maskf(invalidTeamFileError, "team file of %s: entry %d (line %d) is a %s, expected a mapping", team, i, item.Line, kindName(item.Kind))
		}
		if i == 0 {
			// yaml.v3 hands the file's header comment (the yaml-language-server
			// line) to the first entry; it is the file's, not the entry's.
			item.HeadComment = ""
		}
		var name string
		if n := mappingValue(item, "name"); n != nil && n.Kind == yaml.ScalarNode {
			name = n.Value
		}
		tf.Entries = append(tf.Entries, Declaration{Name: name, node: item})
	}

	return tf, nil
}

// Entry returns the entry with the given name.
func (t *TeamFile) Entry(name string) (Declaration, bool) {
	for _, d := range t.Entries {
		if d.Name == name {
			return d, true
		}
	}
	return Declaration{}, false
}

// Instance returns the entry as a JSON-compatible value (maps, slices,
// strings, json.Number, bools), the shape the schema validates.
func (d Declaration) Instance() (any, error) {
	if d.node == nil {
		return nil, microerror.Maskf(invalidConfigError, "declaration %q has no YAML node", d.Name)
	}

	raw, err := yaml.Marshal(d.node)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	j, err := sigsyaml.YAMLToJSON(raw)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return instance, nil
}

// YAML renders the entry as it stands in a team file: one list item.
func (d Declaration) YAML() (string, error) {
	return renderEntry(d.node)
}

// Fields are the fields of an entry the creation rules and the
// scaffold rendering read: what align-files reads when it runs the
// generators for the repository.
type Fields struct {
	Name          string `yaml:"name"`
	ComponentType string `yaml:"componentType"`
	Description   string `yaml:"description"`
	Visibility    string `yaml:"visibility"`
	Lifecycle     string `yaml:"lifecycle"`
	// DefaultBranch is the repository's default branch, main unless
	// declared: the settings step keeps the repository on it and the
	// protection step protects it. A fork line declares the branch that
	// carries the upstream release plus the carried patches.
	DefaultBranch string `yaml:"defaultBranch"`
	// Align is the repository's opt-in to alignment: with it the reconciler
	// changes the repository to its declared set-up on every trigger,
	// without it every run is a check that changes nothing.
	Align          bool     `yaml:"align"`
	ChoreReviewers []string `yaml:"choreReviewers"`
	// RequiredChecks are status-check contexts the protection step requires
	// on the default branch whatever reported, next to the baseline's rule:
	// the repository's own GitHub Actions gate that runs on every pull
	// request. A declared context is never removed as a ghost.
	RequiredChecks []string `yaml:"requiredChecks"`
	// AgentMerge is the repository's opt-out from agent merges: with false
	// the default branch's ruleset has no bypass actor, so nothing merges
	// past the required review. Nil is the default, true.
	AgentMerge *bool `yaml:"agentMerge"`
	// Rulesets names the repository's own rulesets the team keeps beside
	// the engine's, the decision to keep them: the protection step leaves a
	// declared one alone in silence, reports every other one and reports a
	// declared name the repository carries no ruleset for.
	Rulesets []string `yaml:"rulesets"`
	Replace  *struct {
		Precommit bool `yaml:"precommit"`
	} `yaml:"replace"`
	Gen *GenFields `yaml:"gen"`
}

// GenFields is the gen block: the generators' inputs.
type GenFields struct {
	Flavours                      []string  `yaml:"flavours"`
	Language                      string    `yaml:"language"`
	InstallUpdateChart            bool      `yaml:"installUpdateChart"`
	HelmDocsRegen                 bool      `yaml:"helmDocsRegen"`
	RunSecurityScorecard          *bool     `yaml:"runSecurityScorecard"`
	GenerateLlmRules              *bool     `yaml:"generateLlmRules"`
	GoGenerate                    bool      `yaml:"goGenerate"`
	PreCommit                     []string  `yaml:"preCommit"`
	EnableUpstreamSyncAutomation  bool      `yaml:"enableUpstreamSyncAutomation"`
	DispatchUpdateChartEventsRepo string    `yaml:"dispatchUpdateChartEventsRepo"`
	CI                            *CIFields `yaml:"ci"`
}

// CIFields is the gen.ci block: the CircleCI generator's knobs.
type CIFields struct {
	Generate *bool `yaml:"generate"`
	// TemplateContent says the .circleci/config.yml this template repository
	// carries is content for the repositories created from it, not its own
	// pipeline: CircleCI has nothing to build here. The reconciler's circleci
	// and release steps skip the repository. It sits beside Generate false;
	// Generate true beside it is refused.
	TemplateContent bool   `yaml:"templateContent"`
	ReleaseWorkflow string `yaml:"releaseWorkflow"`
	// ReleaseCandidateByDefault makes the auto-release workflow cut a
	// release candidate on every push, leaving the stable release to a
	// manual run.
	ReleaseCandidateByDefault bool   `yaml:"releaseCandidateByDefault"`
	AppCatalog                string `yaml:"appCatalog"`
	AppCatalogTest            string `yaml:"appCatalogTest"`
	ChartName                 string `yaml:"chartName"`
	ChartReleaseGateJob       string `yaml:"chartReleaseGateJob"`
	OverrideChartAppVersion   *bool  `yaml:"overrideChartAppVersion"`
	ForcePublic               bool   `yaml:"forcePublic"`
	Image                     *struct {
		PreBuildJob     string            `yaml:"preBuildJob"`
		PrivateOnly     bool              `yaml:"privateOnly"`
		Name            string            `yaml:"name"`
		Platforms       string            `yaml:"platforms"`
		Dockerfile      string            `yaml:"dockerfile"`
		NativeBuilds    bool              `yaml:"nativeBuilds"`
		ResourceClasses map[string]string `yaml:"resourceClasses"`
	} `yaml:"image"`
	BranchPublish    bool   `yaml:"branchPublish"`
	SkipAppCatalog   bool   `yaml:"skipAppCatalog"`
	SkipATS          bool   `yaml:"skipATS"`
	ATSOnRelease     bool   `yaml:"atsOnRelease"`
	ATSVersion       string `yaml:"atsVersion"`
	ATSResourceClass string `yaml:"atsResourceClass"`
	BuildConcurrency string `yaml:"buildConcurrency"`
	ResourceClass    string `yaml:"resourceClass"`
	Go               *struct {
		TestArtifacts string `yaml:"testArtifacts"`
	} `yaml:"go"`
	Node *struct {
		TestTarget  string `yaml:"testTarget"`
		BuildTarget string `yaml:"buildTarget"`
		BuildOutput string `yaml:"buildOutput"`
	} `yaml:"node"`
}

// fields decodes the fields the creation rules read; a type mismatch is an
// error the schema validation already names.
func (d Declaration) Fields() (Fields, error) {
	var f Fields
	if err := d.node.Decode(&f); err != nil {
		return Fields{}, microerror.Mask(err)
	}
	return f, nil
}

// renderEntry encodes a mapping node as a one-item list, the form an entry
// has in a team file, with the two-space indentation the team files use.
func renderEntry(node *yaml.Node) (string, error) {
	if node == nil {
		return "", microerror.Maskf(invalidConfigError, "no YAML node to render")
	}

	list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{node}}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(list); err != nil {
		return "", microerror.Mask(err)
	}
	if err := enc.Close(); err != nil {
		return "", microerror.Mask(err)
	}

	return buf.String(), nil
}

// mappingValue returns the value node of a key in a mapping node, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMappingValue sets a key of a mapping node, appending the pair when the
// key is absent.
func setMappingValue(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, scalarNode(key), value)
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func boolNode(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)}
}

func mappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

// cloneNode deep-copies a node so a rendering can add defaults without
// changing the team file it was read from.
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Alias = cloneNode(n.Alias)
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			c.Content[i] = cloneNode(child)
		}
	}
	return &c
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "list"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	}
	return fmt.Sprintf("node of kind %d", k)
}
