package updater

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Hold a reader across a refresh, which deterministically exercises the
// snapshot a concurrent Restore opened. Persist must publish a new document
// without truncating or rewriting that reader's document.
func TestCachePersistPreservesOpenReaders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("replacing a file with an open reader depends on Windows sharing flags")
	}
	dir := t.TempDir()
	c := cache{LatestVersion: currentVersion}
	if err := c.Persist(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, cacheFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()

	c.LatestVersion = newerVersion
	if err := c.Persist(dir); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, before) {
		t.Errorf("refresh rewrote an open reader's document: %q, want %q", got, before)
	}
	var restored cache
	if err := restored.Restore(dir); err != nil || restored.LatestVersion != newerVersion {
		t.Errorf("new reader restored %+v, %v; want version %s", restored, err, newerVersion)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != cacheFileMode {
		t.Errorf("cache permissions %o, want %o", info.Mode().Perm(), cacheFileMode)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("refresh left %d files, %v; want only the cache", len(entries), err)
	}
}

func TestCachePersistRemovesTemporaryFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, cacheFileName)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	c := cache{LatestVersion: newerVersion}
	if err := c.Persist(dir); err == nil {
		t.Fatal("persisting over a directory succeeded, want an error")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != cacheFileName || !entries[0].IsDir() {
		t.Errorf("failed refresh changed the destination or left a staging file: %v", entries)
	}
}
