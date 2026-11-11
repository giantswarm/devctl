package reservation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReservationEntryValidatesBeforeWriting(t *testing.T) {
	const value = `'{user: alice, branch: test, scope: app, from: "2026-09-15T10:00:00Z", until: "2026-09-15T11:00:00Z"}'`
	for name, entry := range map[string]string{
		"duplicate decoded key": "app: " + value + "\n  'app': " + value,
		"invalid yaml":          "app: [",
		"invalid reservation":   "app: 'not a reservation'",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "configmap.yaml")
			original := "apiVersion: v1\nkind: ConfigMap\ndata: {}\n"
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := writeReservationEntry(path, "app", entry); err == nil || !IsInvalidConfig(err) {
				t.Fatalf("expected invalid config, got %v", err)
			}
			got, err := os.ReadFile(path) //nolint:gosec // a test fixture
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != original {
				t.Fatalf("invalid output changed the file: %s", got)
			}
		})
	}
}
