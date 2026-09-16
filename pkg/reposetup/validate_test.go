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
		"node-ui", "hello-world-app", "chart-name-mismatch", "internal-visibility", "unknown-field", "taken-name", "renamed-name", "twice", "no-gen",
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
}
