package gen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

// Test_Execute_GenerateFailure_DoesNotTruncateExistingFile guards against a regression where
// execute() opened the destination with os.O_TRUNC before running the Input's Generate/template
// step, so a failure (e.g. Generate's network fetch failing offline, see
// pkg/gen/input/precommit/internal/file/values_schema.go) left a previously-committed file wiped
// to zero bytes even though nothing new was ever written. See giantswarm/devctl#2195, acceptance
// criterion "an offline run ... must not write a truncated schema".
func Test_Execute_GenerateFailure_DoesNotTruncateExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "values.schema.json")
	const original = `{"committed":"content"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	in := input.Input{
		Path:           path,
		SkipRegenCheck: true,
		Generate: func(ctx context.Context) ([]byte, error) {
			return nil, errors.New("simulated network failure")
		},
	}

	if err := execute(context.Background(), in); err == nil {
		t.Fatal("expected execute to return an error")
	}

	got, err := os.ReadFile(path) // #nosec G304 -- t.TempDir() path, test-only
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Errorf("existing file must survive a failed Generate untouched; expected %q, got %q", original, got)
	}
}
