package reposetup

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUndeclaredEntry(t *testing.T) {
	entry := UndeclaredEntry(Undeclared{Name: "my-repo", ComponentType: "service", Lifecycle: "archived"})
	require.True(t, entry.Accepted)
	require.Equal(t, "my-repo", entry.Name)
	require.Equal(t, VerdictUnchecked, entry.NameCheck.Verdict)

	// The rendered entry parses as a one-entry team file the runner reads.
	tf, err := ParseTeamFile("team-bumblebee", strings.NewReader(entry.Rendered))
	require.NoError(t, err)
	require.Len(t, tf.Entries, 1)
	fields, err := tf.Entries[0].Fields()
	require.NoError(t, err)
	require.Equal(t, "my-repo", fields.Name)
	require.Equal(t, "service", fields.ComponentType)
	require.Equal(t, "archived", fields.Lifecycle)

	// The name alone is an entry too.
	tf, err = ParseTeamFile("team-bumblebee", strings.NewReader(UndeclaredEntry(Undeclared{Name: "bare"}).Rendered))
	require.NoError(t, err)
	fields, err = tf.Entries[0].Fields()
	require.NoError(t, err)
	require.Equal(t, Fields{Name: "bare"}, fields)
}

func TestUndeclaredEntryNullRepositoryName(t *testing.T) {
	for _, name := range []string{"null", "Null", "NULL"} {
		t.Run(name, func(t *testing.T) {
			entry := UndeclaredEntry(Undeclared{Name: name})
			tf, err := ParseTeamFile("", strings.NewReader(entry.Rendered))
			require.NoError(t, err)
			require.Len(t, tf.Entries, 1)
			fields, err := tf.Entries[0].Fields()
			require.NoError(t, err)
			require.Equal(t, name, fields.Name, "a repository named null must not decode as YAML null")
		})
	}
}
