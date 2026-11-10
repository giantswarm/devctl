package file

import (
	"os"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The begin marker links to the template, so the template must stay where
// the link says.
func Test_agentsBlockSourceIsThisTemplate(t *testing.T) {
	require.True(t, strings.HasSuffix(agentsBlockSource, "/pkg/gen/input/llm/internal/file/"+agentsBlockTemplateName))
	_, err := os.Stat(path.Base(agentsBlockSource))
	require.NoError(t, err)
}
