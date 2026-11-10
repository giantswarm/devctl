package updater

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCachePersistReplacesCompleteDocument(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("replacement of an open file depends on Windows sharing flags")
	}

	dir := t.TempDir()
	previous := cache{LatestVersion: strings.Repeat("1", 1024)}
	if err := previous.Persist(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, cacheFileName)
	before, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}()

	next := cache{LatestVersion: "2.0.0"}
	if err := next.Persist(dir); err != nil {
		t.Fatal(err)
	}
	retained, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retained, before) {
		t.Fatal("open reader lost the previous complete document")
	}
	var restored cache
	if err := restored.Restore(dir); err != nil {
		t.Fatal(err)
	}
	if restored != next {
		t.Fatalf("restored cache = %+v, want %+v", restored, next)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode = %o, want 600", info.Mode().Perm())
	}
	assertNoCacheStagingFiles(t, dir)
}

func TestCachePersistCleansUpAfterRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, cacheFileName)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "preserved")
	if err := os.WriteFile(marker, []byte("previous content"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := cache{LatestVersion: "2.0.0"}
	if err := c.Persist(dir); err == nil {
		t.Fatal("expected rename over a directory to fail")
	}
	data, err := os.ReadFile(filepath.Clean(marker))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "previous content" {
		t.Fatal("failed publication changed the destination")
	}
	assertNoCacheStagingFiles(t, dir)
}

func assertNoCacheStagingFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != cacheFileName {
		t.Fatalf("unexpected files after persistence: %v", entries)
	}
}
