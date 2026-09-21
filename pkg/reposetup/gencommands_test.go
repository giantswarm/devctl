package reposetup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A fork line carries its upstream's files plus the carried patches: no
// generator runs for it and generated CI has no job for it, whatever its
// language.
func TestGenCommandsNothingForAForkLine(t *testing.T) {
	fork := Fields{Name: "upstream-fork", Gen: &GenFields{Flavours: []string{"fork"}, Language: "go"}}
	require.Empty(t, genCommands(fork, genContext{}))
	require.False(t, hasCIJob(fork))

	service := Fields{Name: "service", Gen: &GenFields{Flavours: []string{"app"}, Language: "go"}}
	require.NotEmpty(t, genCommands(service, genContext{}))
	require.True(t, hasCIJob(service))
}
