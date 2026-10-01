package update

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// recorder answers get_repository with the record and the write with the
// plan, recording the write's arguments.
type recorder struct {
	record string
	args   map[string]any
}

func (r *recorder) Call(_ context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	if tool == manager.ToolGetRepository {
		return json.RawMessage(r.record), nil
	}
	r.args = args
	return json.RawMessage(`{"repository":"giantswarm/my-service","team":"team-bumblebee","accepted":true,"pullRequest":{"title":"chore: edit","branch":"b","files":["f"],"as":"alice"}}`), nil
}

// --set changes the inventory's entry field by field, the value read as
// YAML, and the whole entry goes to the manager with dryRun.
func TestRunSet(t *testing.T) {
	rec := &recorder{record: `{"repository":"giantswarm/my-service","declaration":{"team":"team-bumblebee","file":"f","entry":"- name: my-service\n  componentType: service\n  gen:\n    flavours: [app]\n    language: go\n    ci:\n      generate: true\n"}}`}
	var out bytes.Buffer
	r := &runner{
		flag:   &flag{Flags: write.Flags{Flags: client.Flags{Output: client.OutputText}, DryRun: true}, Set: []string{"gen.ci.generate=false", "align=true", "description=What it does", "gen.flavours=[app, k8sapi]"}, Unset: []string{"gen.language"}},
		logger: logrus.New(), stdout: &out, stderr: &bytes.Buffer{},
		open: func(context.Context) (*client.Session, error) { return &client.Session{Caller: rec}, nil },
	}
	require.NoError(t, r.flag.Validate())
	require.NoError(t, r.run(context.Background(), "my-service"))

	require.Equal(t, true, rec.args["dryRun"])
	require.Equal(t, "my-service", rec.args["repository"])
	entry := rec.args["entry"].(map[string]any)
	require.Equal(t, "my-service", entry["name"])
	require.Equal(t, true, entry["align"])
	require.Equal(t, "What it does", entry["description"])
	gen := entry["gen"].(map[string]any)
	require.Equal(t, []any{"app", "k8sapi"}, gen["flavours"])
	require.Equal(t, map[string]any{"generate": false}, gen["ci"])
	require.NotContains(t, gen, "language")
	require.Contains(t, out.String(), "dry run: nothing written")
}

// --entry-file passes the whole entry, from a list item or a mapping, YAML
// or JSON; an undeclared repository cannot be edited.
func TestEntryFileAndRefusals(t *testing.T) {
	r := &runner{flag: &flag{EntryFile: "-"}, stdin: strings.NewReader(`{"name": "x", "componentType": "tool"}`)}
	entry, err := r.entryFromFile()
	require.NoError(t, err)
	require.Equal(t, map[string]any{"name": "x", "componentType": "tool"}, entry)

	_, err = parseEntry("- a\n- b\n")
	require.True(t, client.IsInvalidFlag(err))

	rec := &recorder{record: `{"repository":"giantswarm/orphan","declaration":null}`}
	r = &runner{flag: &flag{Set: []string{"align=true"}}, logger: logrus.New(), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{},
		open: func(context.Context) (*client.Session, error) { return &client.Session{Caller: rec}, nil }}
	err = r.run(context.Background(), "orphan")
	require.True(t, IsNotDeclared(err), err)
}

// The flags need something to change, and not both ways at once.
func TestValidate(t *testing.T) {
	f := &flag{Flags: write.Flags{Flags: client.Flags{Output: client.OutputText}}}
	require.True(t, client.IsInvalidFlag(f.Validate()))
	f.Set = []string{"align"}
	require.True(t, client.IsInvalidFlag(f.Validate()), "a --set without = is refused")
	f.Set = []string{"align=true"}
	f.EntryFile = "e.yaml"
	require.True(t, client.IsInvalidFlag(f.Validate()))
	f.EntryFile = ""
	require.NoError(t, f.Validate())
}
