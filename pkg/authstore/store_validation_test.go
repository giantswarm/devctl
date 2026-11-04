package authstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRejectsNullRecords(t *testing.T) {
	for _, name := range []string{"get", "set", "delete"} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() != nil {
					t.Error("a null keyring file panicked instead of returning an error")
				}
			}()
			path := filepath.Join(t.TempDir(), "keyring.json")
			if err := os.WriteFile(path, []byte("null\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			s := &FileStore{Path: path}
			var err error
			switch name {
			case "get":
				_, err = s.Get(UserGitHub)
			case "set":
				err = s.Set(UserGitHub, Record{Token: "synthetic-token"})
			case "delete":
				err = s.Delete(UserGitHub)
			}
			if err == nil || errors.Is(err, ErrNotFound) {
				t.Errorf("invalid keyring error = %v, want a corrupt-file error", err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "null\n" { // #nosec G304 -- fixture path under t.TempDir()
				t.Errorf("invalid file was changed: %v", err)
			}
		})
	}
}
