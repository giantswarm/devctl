//go:build !linux

package authstore

// osKeychain is go-keyring's backend: Keychain on macOS, Credential Manager
// on Windows.
func osKeychain() keychain {
	return goKeyring{}
}
