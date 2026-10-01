package authstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rogpeppe/go-internal/lockedfile"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// The keychain coordinates: one service, one record per identity.
const (
	Service      = "devctl"
	UserGitHub   = "github"
	UserCircleCI = "circleci"
	UserMuster   = "muster"
)

// ErrNotFound is returned when the identity has no record.
var ErrNotFound = errors.New("no record in the keychain")

// Record is what the keychain holds for one identity. A zero time means the
// value does not expire.
type Record struct {
	// Login is the account the token acts as, read once at login.
	Login string `json:"login,omitempty"`
	// Token is the access token.
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	// RefreshToken and RefreshExpiresAt: GitHub and muster; muster's refresh
	// token names no expiry.
	RefreshToken     string    `json:"refreshToken,omitempty"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`
	// ClientID and RedirectURI: CircleCI and muster, the per-device OAuth
	// client devctl registered and the loopback address it registered.
	ClientID    string `json:"clientId,omitempty"`
	RedirectURI string `json:"redirectUri,omitempty"`
	// Endpoint and Issuer: muster only, the MCP endpoint the token is for and
	// the authorization server that issued it, so the commands know where the
	// token belongs and a refresh knows whom to ask.
	Endpoint string `json:"endpoint,omitempty"`
	Issuer   string `json:"issuer,omitempty"`
}

// Store keeps the records. Implementations: the OS keychain and, for tests,
// a file.
type Store interface {
	// Get returns the record of user or ErrNotFound.
	Get(user string) (Record, error)
	Set(user string, record Record) error
	// Delete removes the record; a missing record is not an error.
	Delete(user string) error
	// Lock holds user's record for this process and every other devctl on
	// the machine until unlock is called: a refresh reads, trades and writes
	// the record under it, so two runs never spend the same refresh token.
	Lock(user string) (unlock func(), err error)
}

// OpenStore is the store the environment selects: the file of
// [agentcli.EnvKeyringFile] when set, the OS keychain otherwise.
func OpenStore(endpoints agentcli.Endpoints) Store {
	if endpoints.KeyringFile != "" {
		return &FileStore{Path: endpoints.KeyringFile}
	}
	return KeyringStore{}
}

// KeyringStore is the OS keychain: the Secret Service on Linux, Keychain on
// macOS, Credential Manager on Windows. Each record is one JSON secret.
type KeyringStore struct {
	// keychain nil is the OS keychain; tests set go-keyring's mock.
	keychain keychain
}

func (s KeyringStore) backend() keychain {
	if s.keychain != nil {
		return s.keychain
	}
	return osKeychain()
}

// Get implements [Store].
func (s KeyringStore) Get(user string) (Record, error) {
	secret, err := s.backend().get(user)
	if errors.Is(err, ErrNotFound) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("reading %s/%s from the keychain: %w", Service, user, err)
	}
	var r Record
	if err := json.Unmarshal([]byte(secret), &r); err != nil {
		return Record{}, fmt.Errorf("the keychain record %s/%s is not devctl's: %w", Service, user, err)
	}
	return r, nil
}

// Set implements [Store].
func (s KeyringStore) Set(user string, record Record) error {
	b, err := json.Marshal(record) //nolint:gosec // G117: the record goes into the keychain, which is its purpose
	if err != nil {
		return err
	}
	if err := s.backend().set(user, string(b)); err != nil {
		return fmt.Errorf("writing %s/%s to the keychain: %w", Service, user, err)
	}
	return nil
}

// Delete implements [Store].
func (s KeyringStore) Delete(user string) error {
	if err := s.backend().delete(user); err != nil {
		return fmt.Errorf("deleting %s/%s from the keychain: %w", Service, user, err)
	}
	return nil
}

// Lock implements [Store] with a file in the user's cache directory.
func (s KeyringStore) Lock(user string) (func(), error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("locking %s/%s: %w", Service, user, err)
	}
	return lockFile(filepath.Join(dir, Service, user+".lock"))
}

// FileStore keeps the records in one JSON file with mode 0600, for tests and
// machines without a keychain daemon.
type FileStore struct {
	Path string
}

// Lock implements [Store] with a file beside the store's.
func (s *FileStore) Lock(user string) (func(), error) {
	return lockFile(s.Path + "." + user + ".lock")
}

// lockFile holds the file lock at path, creating the file and its directory.
func lockFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating the directory of the lock %s: %w", path, err)
	}
	unlock, err := lockedfile.MutexAt(path).Lock()
	if err != nil {
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return unlock, nil
}

// Get implements [Store].
func (s *FileStore) Get(user string) (Record, error) {
	records, err := s.read()
	if err != nil {
		return Record{}, err
	}
	r, ok := records[user]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}

// Set implements [Store].
func (s *FileStore) Set(user string, record Record) error {
	records, err := s.read()
	if err != nil {
		return err
	}
	records[user] = record
	return s.write(records)
}

// Delete implements [Store].
func (s *FileStore) Delete(user string) error {
	records, err := s.read()
	if err != nil {
		return err
	}
	if _, ok := records[user]; !ok {
		return nil
	}
	delete(records, user)
	return s.write(records)
}

func (s *FileStore) read() (map[string]Record, error) {
	records := map[string]Record{}
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return records, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the keyring file %s: %w", s.Path, err)
	}
	if len(b) == 0 {
		return records, nil
	}
	if err := json.Unmarshal(b, &records); err != nil {
		return nil, fmt.Errorf("the keyring file %s is not devctl's: %w", s.Path, err)
	}
	return records, nil
}

func (s *FileStore) write(records map[string]Record) error {
	b, err := json.MarshalIndent(records, "", "  ") //nolint:gosec // G117: the records go into the 0600 file that replaces the keychain
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.Path, b, 0o600); err != nil {
		return fmt.Errorf("writing the keyring file %s: %w", s.Path, err)
	}
	// WriteFile keeps the mode of an existing file; the tokens are ours alone.
	if err := os.Chmod(s.Path, 0o600); err != nil {
		return fmt.Errorf("securing the keyring file %s: %w", s.Path, err)
	}
	return nil
}
