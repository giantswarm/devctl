package reposetup

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTeamOf(t *testing.T) {
	require.Equal(t, "team-bumblebee", TeamOf("repositories/team-bumblebee.yaml"))
	require.Equal(t, "chapter-se", TeamOf("/abs/path/repositories/chapter-se.yaml"))
	require.Equal(t, "team-rocket", TeamOf("team-rocket.yml"))
}

func TestParseTeamFile(t *testing.T) {
	t.Run("entries in order, names read", func(t *testing.T) {
		tf, err := ParseTeamFile("team-rocket", strings.NewReader("- name: a\n  componentType: cli\n- componentType: cli\n- name: c\n"))
		require.NoError(t, err)
		require.Equal(t, "team-rocket", tf.Team)
		require.Len(t, tf.Entries, 3)
		require.Equal(t, "a", tf.Entries[0].Name)
		require.Equal(t, "", tf.Entries[1].Name, "an entry without a name is kept for the schema to refuse")
		require.Equal(t, "c", tf.Entries[2].Name)

		d, ok := tf.Entry("c")
		require.True(t, ok)
		require.Equal(t, "c", d.Name)
		_, ok = tf.Entry("z")
		require.False(t, ok)
	})

	t.Run("empty file is a team without entries", func(t *testing.T) {
		tf, err := ParseTeamFile("team-rocket", strings.NewReader("# nothing yet\n"))
		require.NoError(t, err)
		require.Empty(t, tf.Entries)
	})

	t.Run("top level must be a list", func(t *testing.T) {
		_, err := ParseTeamFile("team-rocket", strings.NewReader("name: a\n"))
		require.True(t, IsInvalidTeamFile(err), "%v", err)
		require.Contains(t, err.Error(), "expected a list of entries at the top level, got a mapping")
	})

	t.Run("entries must be mappings", func(t *testing.T) {
		_, err := ParseTeamFile("team-rocket", strings.NewReader("- name: a\n- just-a-string\n"))
		require.True(t, IsInvalidTeamFile(err), "%v", err)
		require.Contains(t, err.Error(), "entry 1 (line 2) is a scalar, expected a mapping")
	})

	t.Run("invalid yaml", func(t *testing.T) {
		_, err := ParseTeamFile("team-rocket", strings.NewReader("- name: [\n"))
		require.True(t, IsInvalidTeamFile(err), "%v", err)
	})
}

// Fields carries the repository's opt-in to alignment; an entry without the
// field is not opted in.
func TestDeclarationFieldsAlign(t *testing.T) {
	tf, err := ParseTeamFile("team-rocket", strings.NewReader("- name: a\n  componentType: cli\n  align: true\n- name: b\n  componentType: cli\n"))
	require.NoError(t, err)
	require.Len(t, tf.Entries, 2)

	fields, err := tf.Entries[0].Fields()
	require.NoError(t, err)
	require.True(t, fields.Align)

	fields, err = tf.Entries[1].Fields()
	require.NoError(t, err)
	require.False(t, fields.Align)
}

func TestDeclarationInstanceAndYAML(t *testing.T) {
	tf, err := ParseTeamFile("team-rocket", strings.NewReader("- name: a\n  gen:\n    flavours: [app]\n    ci:\n      generate: false\n"))
	require.NoError(t, err)

	instance, err := tf.Entries[0].Instance()
	require.NoError(t, err)
	obj, ok := instance.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "a", obj["name"])
	require.Equal(t, false, obj["gen"].(map[string]any)["ci"].(map[string]any)["generate"])

	raw, err := tf.Entries[0].YAML()
	require.NoError(t, err)
	require.Equal(t, "- name: a\n  gen:\n    flavours: [app]\n    ci:\n      generate: false\n", raw)
}
