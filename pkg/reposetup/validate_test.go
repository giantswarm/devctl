package reposetup

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeNames answers the name check from a table; every other name is free.
type fakeNames map[string]NameCheck

func (f fakeNames) CheckName(_ context.Context, owner, name string) (NameCheck, error) {
	if check, ok := f[name]; ok {
		return check, nil
	}
	return NameCheck{Verdict: VerdictFree, Detail: "no repository " + owner + "/" + name + " on GitHub"}, nil
}

func fixtureValidator(t *testing.T) (Validator, *TeamFile) {
	t.Helper()

	schema, err := EmbeddedSchema()
	require.NoError(t, err)

	tf, err := ReadTeamFile(filepath.Join("testdata", "team-bumblebee.yaml"))
	require.NoError(t, err)
	require.Equal(t, "team-bumblebee", tf.Team)

	v := Validator{
		Schema: schema,
		Names: fakeNames{
			"taken-name":   {Verdict: VerdictTaken, Detail: "repository giantswarm/taken-name exists"},
			"renamed-name": {Verdict: VerdictTaken, Detail: "giantswarm/renamed-name redirects to giantswarm/new-name: the name belongs to a renamed repository"},
		},
	}
	return v, tf
}

func TestValidateEntries(t *testing.T) {
	v, tf := fixtureValidator(t)

	tests := []struct {
		name     string
		template Template
		verdict  Verdict
		// fields the problems name, sorted; nil for an accepted entry
		fields []string
	}{
		{name: "good-service", template: TemplateGo, verdict: VerdictFree},
		{name: "good-chart", template: TemplateChart, verdict: VerdictFree},
		{name: "good-cli", template: TemplateGo, verdict: VerdictFree},
		{name: "minimal-config", template: TemplateMinimal, verdict: VerdictFree},
		{name: "customer-configs", template: TemplateMinimal, verdict: VerdictFree},
		{name: "upstream-fork", template: TemplateMinimal, verdict: VerdictFree},
		{name: "policies", template: TemplateMinimal, verdict: VerdictFree},
		{name: "python-tool", template: TemplateMinimal, verdict: VerdictFree},
		{name: "hello-world-app", template: TemplateChart, verdict: VerdictFree, fields: []string{"name"}},
		{name: "no-gen", verdict: VerdictFree, fields: []string{"gen.flavours", "gen.language"}},
		{name: "empty-gen", verdict: VerdictFree, fields: []string{"gen.flavours", "gen.language"}},
		{name: "node-ui", verdict: VerdictFree, fields: []string{"gen.language"}},
		{name: "Bad_Name", template: TemplateMinimal, verdict: VerdictUnchecked, fields: []string{"name"}},
		{name: "chart-name-mismatch", template: TemplateChart, verdict: VerdictFree, fields: []string{"gen.ci.chartName"}},
		{name: "internal-visibility", template: TemplateGo, verdict: VerdictFree, fields: []string{"lifecycle", "visibility"}},
		{name: "unknown-field", template: TemplateGo, verdict: VerdictFree, fields: []string{"template"}},
		{name: "unknown-flavour", verdict: VerdictFree, fields: []string{"gen.flavours[0]"}},
		{name: "twice", template: TemplateGo, verdict: VerdictFree, fields: []string{"name"}},
		{name: "taken-name", template: TemplateGo, verdict: VerdictTaken, fields: []string{"name"}},
		{name: "renamed-name", template: TemplateGo, verdict: VerdictTaken, fields: []string{"name"}},
		{name: "no-ci-jobs", template: TemplateMinimal, verdict: VerdictFree, fields: []string{"gen.ci.generate"}},
		{name: "align-opt-in", template: TemplateGo, verdict: VerdictFree},
		{name: "align-not-bool", template: TemplateGo, verdict: VerdictFree, fields: []string{"align"}},
		{name: "agent-merge-opt-out", template: TemplateGo, verdict: VerdictFree},
		{name: "agent-merge-not-bool", template: TemplateGo, verdict: VerdictFree, fields: []string{"agentMerge"}},
		{name: "ci-without-generate", template: TemplateChart, verdict: VerdictFree},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := v.Validate(context.Background(), Request{TeamFile: tf, Names: []string{tc.name}})
			require.NoError(t, err)
			require.Len(t, result.Entries, 1)
			entry := result.Entries[0]

			require.Equal(t, tc.name, entry.Name)
			require.Equal(t, tc.template, entry.Template)
			require.Equal(t, tc.verdict, entry.NameCheck.Verdict)
			require.Equal(t, problemFields(entry.Problems), tc.fields, "problems: %v", entry.Problems)
			require.Equal(t, tc.fields == nil, entry.Accepted)
			require.Equal(t, tc.fields == nil, result.Accepted)
			require.Equal(t, SchemaOriginEmbedded, result.Schema)
			require.Equal(t, "team-bumblebee", result.Team)
			require.True(t, strings.HasPrefix(entry.Rendered, "- name: "+tc.name+"\n"), "rendered: %q", entry.Rendered)
		})
	}
}

func problemFields(problems []Problem) []string {
	if len(problems) == 0 {
		return nil
	}
	fields := map[string]bool{}
	for _, p := range problems {
		fields[p.Field] = true
	}
	out := make([]string, 0, len(fields))
	for f := range fields {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func TestValidateMessagesNameTheReason(t *testing.T) {
	v, tf := fixtureValidator(t)
	result, err := v.Validate(context.Background(), Request{TeamFile: tf, Names: []string{
		"node-ui", "hello-world-app", "chart-name-mismatch", "internal-visibility", "unknown-field", "unknown-flavour", "taken-name", "renamed-name", "twice", "no-gen",
	}})
	require.NoError(t, err)

	messages := map[string]string{}
	for _, entry := range result.Entries {
		for _, p := range entry.Problems {
			messages[entry.Name+"/"+p.Field] = p.Message
		}
	}

	require.Equal(t, "the Node template is not available yet", messages["node-ui/gen.language"])
	require.Contains(t, messages["hello-world-app/name"], "without the -app suffix")
	require.Contains(t, messages["chart-name-mismatch/gen.ci.chartName"], `must equal the repository name "chart-name-mismatch"`)
	require.Contains(t, messages["internal-visibility/visibility"], "public")
	require.Contains(t, messages["internal-visibility/lifecycle"], "archived")
	require.Equal(t, "not a field of the repositories schema", messages["unknown-field/template"])
	require.Equal(t, "value must be one of 'app', 'cli', 'cluster-app', 'customer', 'fleet', 'fork', 'generic', 'k8sapi'", messages["unknown-flavour/gen.flavours[0]"])
	require.Equal(t, "taken: repository giantswarm/taken-name exists", messages["taken-name/name"])
	require.Contains(t, messages["renamed-name/name"], "redirects to giantswarm/new-name")
	require.Contains(t, messages["twice/name"], "declared more than once")
	require.Equal(t, "required for a repository the reconciler creates", messages["no-gen/gen.flavours"])
}

func TestRenderedEntryCarriesTheDefaults(t *testing.T) {
	v, tf := fixtureValidator(t)

	result, err := v.Validate(context.Background(), Request{TeamFile: tf, Names: []string{"good-chart", "good-service"}})
	require.NoError(t, err)
	require.True(t, result.Accepted)

	// The ci block is added with generate: true, after the fields the author wrote.
	require.Equal(t, `- name: good-chart
  # A chart-only repository: the ci block is left out, generate defaults to true.
  componentType: service
  gen:
    flavours:
      - app
    language: generic
    ci:
      generate: true
`, result.Entries[0].Rendered)

	// An explicit ci block is left as it is.
	require.Equal(t, `- name: good-service
  description: A Go service with a chart.
  visibility: public
  system: agent-platform
  componentType: service
  gen:
    flavours:
      - app
    language: go
    ci:
      generate: true
      releaseWorkflow: auto-release
`, result.Entries[1].Rendered)

	// Validated for a repository that exists, the entry is rendered as
	// declared: the creation default is not written, and gen.ci left out
	// says the repository keeps its own CircleCI configuration.
	existing, err := v.Validate(context.Background(), Request{TeamFile: tf, Names: []string{"good-chart"}, Mode: ModeExisting})
	require.NoError(t, err)
	require.True(t, existing.Accepted)
	require.Equal(t, `- name: good-chart
  # A chart-only repository: the ci block is left out, generate defaults to true.
  componentType: service
  gen:
    flavours:
      - app
    language: generic
`, existing.Entries[0].Rendered)

	// The team file itself is untouched: a second rendering of the source is unchanged.
	d, ok := tf.Entry("good-chart")
	require.True(t, ok)
	raw, err := d.YAML()
	require.NoError(t, err)
	require.NotContains(t, raw, "generate: true")
}

func TestValidateAllEntriesWithoutNames(t *testing.T) {
	v, tf := fixtureValidator(t)

	result, err := v.Validate(context.Background(), Request{TeamFile: tf})
	require.NoError(t, err)
	require.Len(t, result.Entries, len(tf.Entries))
	require.False(t, result.Accepted)
}

func TestValidateUnknownEntry(t *testing.T) {
	v, tf := fixtureValidator(t)

	_, err := v.Validate(context.Background(), Request{TeamFile: tf, Names: []string{"good-chart", "nope"}})
	require.True(t, IsEntryNotFound(err), "%v", err)
	require.Contains(t, err.Error(), `"nope"`)
}

func TestValidateRequiresSchemaAndTeamFile(t *testing.T) {
	_, err := Validator{}.Validate(context.Background(), Request{TeamFile: &TeamFile{}})
	require.True(t, IsInvalidConfig(err))

	schema, err := EmbeddedSchema()
	require.NoError(t, err)
	_, err = Validator{Schema: schema}.Validate(context.Background(), Request{})
	require.True(t, IsInvalidConfig(err))
}

func TestNotices(t *testing.T) {
	v, tf := fixtureValidator(t)
	ctx := context.Background()
	kinds := func(notices []Notice) []NoticeKind {
		out := make([]NoticeKind, 0, len(notices))
		for _, n := range notices {
			out = append(out, n.Kind)
		}
		return out
	}
	three := []string{"good-service", "good-chart", "good-cli"}
	four := append(three, "minimal-config")

	t.Run("member of the team", func(t *testing.T) {
		result, err := v.Validate(ctx, Request{TeamFile: tf, Names: three, Author: "someone", AuthorTeams: []string{"@giantswarm/team-bumblebee"}})
		require.NoError(t, err)
		require.Empty(t, result.Notices)
		require.True(t, result.Accepted)
	})

	t.Run("member of the fallback team", func(t *testing.T) {
		result, err := v.Validate(ctx, Request{TeamFile: tf, Names: three, Author: "someone", AuthorTeams: []string{"team-planeteers", "team-rocket"}})
		require.NoError(t, err)
		require.Empty(t, result.Notices)
	})

	t.Run("outsider keeps the team's review", func(t *testing.T) {
		result, err := v.Validate(ctx, Request{TeamFile: tf, Names: three, Author: "someone", AuthorTeams: []string{"team-rocket"}})
		require.NoError(t, err)
		require.Equal(t, []NoticeKind{NoticeTeamReview}, kinds(result.Notices))
		require.Contains(t, result.Notices[0].Message, "your team's review will be required")
		require.Contains(t, result.Notices[0].Message, "someone is not a member of team-bumblebee or team-planeteers")
		require.True(t, result.Accepted, "a notice is not a refusal")
	})

	t.Run("unknown author skips the team guard", func(t *testing.T) {
		result, err := v.Validate(ctx, Request{TeamFile: tf, Names: three})
		require.NoError(t, err)
		require.Empty(t, result.Notices)
	})

	t.Run("more than three entries get a person", func(t *testing.T) {
		result, err := v.Validate(ctx, Request{TeamFile: tf, Names: four, Author: "someone", AuthorTeams: []string{"team-bumblebee"}})
		require.NoError(t, err)
		require.Equal(t, []NoticeKind{NoticeBatchReview}, kinds(result.Notices))
		require.Contains(t, result.Notices[0].Message, "a person will review: 4 entries are added and the machine approves at most 3")
	})

	t.Run("no name checker", func(t *testing.T) {
		unchecked := Validator{Schema: v.Schema}
		result, err := unchecked.Validate(ctx, Request{TeamFile: tf, Names: []string{"good-chart"}})
		require.NoError(t, err)
		require.Equal(t, []NoticeKind{NoticeNamesUnchecked}, kinds(result.Notices))
		require.Equal(t, VerdictUnchecked, result.Entries[0].NameCheck.Verdict)
		require.True(t, result.Accepted)
	})

	t.Run("existing entries get no review guard", func(t *testing.T) {
		result, err := v.Validate(ctx, Request{TeamFile: tf, Names: four, Author: "someone", AuthorTeams: []string{"team-rocket"}, Mode: ModeExisting})
		require.NoError(t, err)
		require.Empty(t, result.Notices)

		unchecked := Validator{Schema: v.Schema}
		result, err = unchecked.Validate(ctx, Request{TeamFile: tf, Names: four, Author: "someone", AuthorTeams: []string{"team-rocket"}, Mode: ModeExisting})
		require.NoError(t, err)
		require.Equal(t, []NoticeKind{NoticeNamesUnchecked}, kinds(result.Notices))
		require.Equal(t, "not checked: no GitHub client", result.Entries[0].NameCheck.Detail)
	})
}

func TestValidateExistingMode(t *testing.T) {
	v, tf := fixtureValidator(t)

	tests := []struct {
		name     string
		template Template
		verdict  Verdict
		// fields the problems name, sorted; nil for an accepted entry
		fields []string
	}{
		{name: "good-service", template: TemplateGo, verdict: VerdictFree},
		{name: "good-chart", template: TemplateChart, verdict: VerdictFree},
		{name: "upstream-fork", template: TemplateMinimal, verdict: VerdictFree},
		// The creation rules do not apply: entries that predate gen, the
		// chart-name convention, an unavailable template, a taken name.
		{name: "no-gen", verdict: VerdictFree},
		{name: "node-ui", verdict: VerdictFree},
		{name: "hello-world-app", template: TemplateChart, verdict: VerdictFree},
		{name: "chart-name-mismatch", template: TemplateChart, verdict: VerdictFree},
		{name: "Bad_Name", template: TemplateMinimal, verdict: VerdictFree},
		{name: "no-ci-jobs", template: TemplateMinimal, verdict: VerdictFree},
		{name: "taken-name", template: TemplateGo, verdict: VerdictTaken},
		{name: "renamed-name", template: TemplateGo, verdict: VerdictTaken},
		// The schema and the file's integrity still refuse: gen, once
		// present, needs flavours and language by the schema, and a flavour
		// devctl has no generator for is not in the schema's enum.
		{name: "empty-gen", verdict: VerdictFree, fields: []string{"gen.language"}},
		{name: "unknown-flavour", verdict: VerdictFree, fields: []string{"gen.flavours[0]"}},
		{name: "internal-visibility", template: TemplateGo, verdict: VerdictFree, fields: []string{"lifecycle", "visibility"}},
		{name: "unknown-field", template: TemplateGo, verdict: VerdictFree, fields: []string{"template"}},
		{name: "twice", template: TemplateGo, verdict: VerdictFree, fields: []string{"name"}},
		// The entry is rendered as declared: no creation default fills a ci
		// block the schema finds incomplete.
		{name: "ci-without-generate", template: TemplateChart, verdict: VerdictFree, fields: []string{"gen.ci.generate"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := v.Validate(context.Background(), Request{TeamFile: tf, Names: []string{tc.name}, Mode: ModeExisting})
			require.NoError(t, err)
			require.Len(t, result.Entries, 1)
			entry := result.Entries[0]

			require.Equal(t, ModeExisting, result.Mode)
			require.Equal(t, tc.template, entry.Template)
			require.Equal(t, tc.verdict, entry.NameCheck.Verdict)
			require.Equal(t, problemFields(entry.Problems), tc.fields, "problems: %v", entry.Problems)
			require.Equal(t, tc.fields == nil, entry.Accepted)
			require.True(t, strings.HasPrefix(entry.Rendered, "- name: "+tc.name+"\n"), "rendered: %q", entry.Rendered)
		})
	}

	t.Run("the whole file: only the schema and the duplicate refuse", func(t *testing.T) {
		result, err := v.Validate(context.Background(), Request{TeamFile: tf, Mode: ModeExisting})
		require.NoError(t, err)
		require.Len(t, result.Entries, len(tf.Entries))
		var refused []string
		for _, entry := range result.Entries {
			if !entry.Accepted {
				refused = append(refused, entry.Name)
			}
		}
		require.Equal(t, []string{"empty-gen", "internal-visibility", "unknown-field", "unknown-flavour", "twice", "twice", "align-not-bool", "agent-merge-not-bool", "ci-without-generate"}, refused)
		require.False(t, result.Accepted)
	})

	t.Run("a missing repository is not a refusal", func(t *testing.T) {
		result, err := v.Validate(context.Background(), Request{TeamFile: tf, Names: []string{"good-service"}, Mode: ModeExisting})
		require.NoError(t, err)
		require.Equal(t, VerdictFree, result.Entries[0].NameCheck.Verdict)
		require.True(t, result.Entries[0].Accepted, "the reconciler reports it, the validation does not refuse it")
	})
}

func TestValidateMode(t *testing.T) {
	v, tf := fixtureValidator(t)
	ctx := context.Background()

	t.Run("empty means create", func(t *testing.T) {
		result, err := v.Validate(ctx, Request{TeamFile: tf, Names: []string{"no-gen"}})
		require.NoError(t, err)
		require.Equal(t, ModeCreate, result.Mode)
		require.False(t, result.Accepted)
	})

	t.Run("create, said so", func(t *testing.T) {
		result, err := v.Validate(ctx, Request{TeamFile: tf, Names: []string{"no-gen"}, Mode: ModeCreate})
		require.NoError(t, err)
		require.Equal(t, ModeCreate, result.Mode)
		require.Equal(t, []string{"gen.flavours", "gen.language"}, problemFields(result.Entries[0].Problems))
	})

	t.Run("unknown mode", func(t *testing.T) {
		_, err := v.Validate(ctx, Request{TeamFile: tf, Mode: "repair"})
		require.True(t, IsInvalidConfig(err), "%v", err)
		require.Contains(t, err.Error(), "want create or existing")
	})
}
