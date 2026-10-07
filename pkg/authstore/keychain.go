package authstore

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// keychain is the OS keychain behind [KeyringStore]: one secret per user of
// [Service]. get returns [ErrNotFound] for a missing user; delete of a missing
// user is not an error.
type keychain interface {
	get(user string) (string, error)
	set(user, secret string) error
	delete(user string) error
}

// goKeyring is the keychain through github.com/zalando/go-keyring: Keychain
// on macOS, Credential Manager on Windows, and its in-memory mock in tests.
type goKeyring struct{}

func (goKeyring) get(user string) (string, error) {
	s, err := keyring.Get(Service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return s, err
}

func (goKeyring) set(user, secret string) error {
	return keyring.Set(Service, user, secret)
}

func (goKeyring) delete(user string) error {
	err := keyring.Delete(Service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
