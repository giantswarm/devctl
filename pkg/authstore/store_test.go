package authstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestFileStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring.json")
	s := OpenStore(agentcli.Endpoints{KeyringFile: path})
	if _, ok := s.(*FileStore); !ok {
		t.Fatalf("OpenStore with a file = %T", s)
	}

	if _, err := s.Get(UserGitHub); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty store Get = %v", err)
	}
	if err := s.Delete(UserGitHub); err != nil {
		t.Fatalf("Delete on an empty store = %v", err)
	}

	want := Record{
		Login:            "octocat",
		Token:            "ghu_secret",
		ExpiresAt:        time.Date(2026, 9, 21, 18, 0, 0, 0, time.UTC),
		RefreshToken:     "ghr_secret",
		RefreshExpiresAt: time.Date(2027, 3, 21, 10, 0, 0, 0, time.UTC),
	}
	if err := s.Set(UserGitHub, want); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(UserCircleCI, Record{Token: "ccipat_secret", ClientID: "client-1", RedirectURI: "http://127.0.0.1:4242/callback"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("keyring file mode = %o, want 600", mode)
	}

	got, err := s.Get(UserGitHub)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) || !got.RefreshExpiresAt.Equal(want.RefreshExpiresAt) {
		t.Fatalf("times: got %+v", got)
	}
	got.ExpiresAt, got.RefreshExpiresAt = want.ExpiresAt, want.RefreshExpiresAt
	if got != want {
		t.Fatalf("Get = %+v, want %+v", got, want)
	}

	if err := s.Delete(UserGitHub); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(UserGitHub); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete: %v", err)
	}
	if other, err := s.Get(UserCircleCI); err != nil || other.ClientID != "client-1" {
		t.Fatalf("the other record went with it: %+v %v", other, err)
	}

	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(UserCircleCI); err == nil {
		t.Fatal("a corrupt file must be an error, not an empty store")
	}
}

func TestKeyringStore(t *testing.T) {
	keyring.MockInit()
	s := OpenStore(agentcli.Endpoints{})
	if _, ok := s.(KeyringStore); !ok {
		t.Fatalf("OpenStore without a file = %T", s)
	}
	if _, err := s.Get(UserCircleCI); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty keyring Get = %v", err)
	}
	want := Record{Login: "octocat", Token: "ccipat_secret", ClientID: "client-1"}
	if err := s.Set(UserCircleCI, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(UserCircleCI)
	if err != nil || got.Token != want.Token || got.ClientID != want.ClientID || got.Login != want.Login {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if err := s.Delete(UserCircleCI); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(UserCircleCI); err != nil {
		t.Fatalf("second Delete = %v", err)
	}
	if _, err := s.Get(UserCircleCI); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete: %v", err)
	}
}
