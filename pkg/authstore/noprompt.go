package authstore

import (
	"errors"
	"sync/atomic"
)

// noPrompt is set by [NoPrompt].
var noPrompt atomic.Bool

// ErrKeychainLocked is a keychain read that would have to prompt the person
// to unlock it, after [NoPrompt].
var ErrKeychainLocked = errors.New("the keychain is locked and devctl may not prompt to unlock it")

// NoPrompt makes every later keychain read of the process fail with
// [ErrKeychainLocked] where it would prompt the person to unlock the
// keychain: for work nobody asked for, such as the CircleCI pass after a
// command. It cannot be undone.
func NoPrompt() {
	noPrompt.Store(true)
}
