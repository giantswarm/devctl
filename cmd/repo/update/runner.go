package update

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// readTimeout bounds the read of the inventory record --set starts from.
const readTimeout = 60 * time.Second

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader
	open   client.Opener
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(r.run(context.Background(), args[0]))
}

func (r *runner) run(ctx context.Context, arg string) error {
	repository, err := client.Repository(arg)
	if err != nil {
		return microerror.Mask(err)
	}
	session, err := r.open(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	var entry map[string]any
	if r.flag.EntryFile != "" {
		entry, err = r.entryFromFile()
	} else {
		entry, err = r.entryFromInventory(ctx, session, repository)
	}
	if err != nil {
		return microerror.Mask(err)
	}

	args := map[string]any{"repository": repository, "entry": entry}
	return microerror.Mask(write.Run(ctx, session, r.stdout, &r.flag.Flags, manager.ToolUpdateRepository, args))
}

// entryFromFile reads the whole entry from --entry-file.
func (r *runner) entryFromFile() (map[string]any, error) {
	var data []byte
	var err error
	if r.flag.EntryFile == "-" {
		data, err = io.ReadAll(r.stdin)
	} else {
		data, err = os.ReadFile(filepath.Clean(r.flag.EntryFile))
	}
	if err != nil {
		return nil, microerror.Maskf(client.InvalidFlagError, "reading --entry-file: %v", err)
	}
	entry, err := parseEntry(string(data))
	if err != nil {
		return nil, microerror.Maskf(client.InvalidFlagError, "--entry-file: %v", err)
	}
	return entry, nil
}

// entryFromInventory reads the entry as the inventory holds it and applies
// --set and --unset.
func (r *runner) entryFromInventory(ctx context.Context, session *client.Session, repository string) (map[string]any, error) {
	callCtx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	payload, err := session.Call(callCtx, manager.ToolGetRepository, map[string]any{"repository": repository})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	record, err := client.Decode[manager.Record](manager.ToolGetRepository, payload)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if record.Declaration == nil || strings.TrimSpace(record.Declaration.Entry) == "" {
		return nil, microerror.Maskf(notDeclaredError, "%s is not declared in any team file; declare it with `devctl repo adopt`", repository)
	}
	entry, err := parseEntry(record.Declaration.Entry)
	if err != nil {
		return nil, microerror.Maskf(client.UnexpectedAnswerError, "the inventory's entry of %s: %v", repository, err)
	}
	for _, s := range r.flag.Set {
		path, value, err := splitSet(s)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		if err := setPath(entry, path, value); err != nil {
			return nil, microerror.Mask(err)
		}
	}
	for _, path := range r.flag.Unset {
		unsetPath(entry, strings.Split(path, "."))
	}
	return entry, nil
}

// parseEntry reads an entry from YAML or JSON: the team file's list item
// (one item) or the mapping itself.
func parseEntry(text string) (map[string]any, error) {
	var v any
	if err := yaml.Unmarshal([]byte(text), &v); err != nil {
		return nil, microerror.Maskf(client.InvalidFlagError, "not YAML: %v", err)
	}
	switch e := v.(type) {
	case map[string]any:
		return e, nil
	case []any:
		if len(e) == 1 {
			if m, ok := e[0].(map[string]any); ok {
				return m, nil
			}
		}
	}
	return nil, microerror.Maskf(client.InvalidFlagError, "an entry is one mapping (or a list of one)")
}

// splitSet reads path=value; the value is YAML, so true is a boolean and
// [a, b] a list.
func splitSet(s string) (path []string, value any, err error) {
	key, raw, ok := strings.Cut(s, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return nil, nil, microerror.Maskf(client.InvalidFlagError, "--set wants path=value, got %q", s)
	}
	if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
		return nil, nil, microerror.Maskf(client.InvalidFlagError, "--set %s: the value is not YAML: %v", key, err)
	}
	return strings.Split(key, "."), value, nil
}

// setPath writes value at the dotted path, creating the mappings on the way.
func setPath(m map[string]any, path []string, value any) error {
	for i, key := range path[:len(path)-1] {
		next, ok := m[key]
		if !ok || next == nil {
			child := map[string]any{}
			m[key] = child
			m = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return microerror.Maskf(client.InvalidFlagError, "--set %s: %s is not a mapping", strings.Join(path, "."), strings.Join(path[:i+1], "."))
		}
		m = child
	}
	m[path[len(path)-1]] = value
	return nil
}

// unsetPath removes the dotted path; a path that does not exist is nothing
// to do.
func unsetPath(m map[string]any, path []string) {
	for _, key := range path[:len(path)-1] {
		child, ok := m[key].(map[string]any)
		if !ok {
			return
		}
		m = child
	}
	delete(m, path[len(path)-1])
}

var notDeclaredError = &microerror.Error{
	Kind: "notDeclaredError",
}

// IsNotDeclared asserts notDeclaredError.
func IsNotDeclared(err error) bool {
	return microerror.Cause(err) == notDeclaredError
}
