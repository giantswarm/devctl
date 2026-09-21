//go:build linux

package authstore

// osKeychain is the Secret Service of the session bus, spoken directly:
// GNOME Keyring, KWallet, oo7-daemon and KeePassXC alike.
func osKeychain() keychain {
	return secretService{}
}
