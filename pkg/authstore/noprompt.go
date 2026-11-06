package authstore

import (
	"errors"
	"sync/atomic"
)

// noPrompt is set by [NoPrompt].
var noPrompt atomic.Bool

// ErrKeychainLocked is a keychain call that would have to prompt the person
// (to unlock the keychain, or to confirm a write), after [NoPrompt].
var ErrKeychainLocked = errors.New("the keychain wants to prompt and devctl may not")

// NoPrompt makes every later Secret Service call of the process fail with
// [ErrKeychainLocked] where the service returns a prompt (an unlock, a
// confirmation): for work nobody asked for, such as the CircleCI pass after a
// command. It cannot be undone. The macOS keychain and a service that asks
// inside the call (KeePassXC's per-item confirmation) are not covered; only
// the caller's timeout bounds them.
func NoPrompt() {
	noPrompt.Store(true)
}
