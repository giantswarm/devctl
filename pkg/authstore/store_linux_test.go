//go:build linux

package authstore

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A failed write must leave every previously stored identity intact. A child
// process limits its own file size to force a short write on an ordinary file.
func TestFileStoreFailedWritePreservesRecords(t *testing.T) {
	if len(os.Args) >= 2 && os.Args[len(os.Args)-2] == "authstore-limited-write" {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 512, Max: 512}); err != nil {
			t.Fatal(err)
		}
		s := &FileStore{Path: os.Args[len(os.Args)-1]}
		err := s.Set(UserGitHub, Record{Token: strings.Repeat("synthetic", 512)})
		if !errors.Is(err, syscall.EFBIG) {
			t.Fatalf("limited write error = %v, want EFBIG", err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "keyring.json")
	s := &FileStore{Path: path}
	if err := s.Set(UserCircleCI, Record{Token: "synthetic-circleci"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestFileStoreFailedWritePreservesRecords$", "--", "authstore-limited-write", path)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("limited writer: %v: %s", err, out)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed Set changed the existing keyring file")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".devctl-keyring-") {
			t.Error("failed Set left a temporary secret file")
		}
	}
}
