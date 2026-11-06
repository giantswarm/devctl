package authstore

import (
	"errors"
	"sync/atomic"
)

// noPrompt is set by [NoPrompt].
var noPrompt atomic.Bool

// ErrKeychainLocked is a keychain call that would have to prompt the person
// (to unlock the keychain, or to confirm a delete), after [NoPrompt].
var ErrKeychainLocked = errors.New("the keychain wants to prompt and devctl may not")

// NoPrompt makes every later Secret Service unlock or delete of the process
// fail with [ErrKeychainLocked] where the service returns a prompt: for work
// nobody asked for, such as the CircleCI pass after a command. It cannot be
// undone. A write's prompt still runs, since the record (a refreshed token
// pair) may replace one already spent; left unanswered until the caller
// gives up, that write is lost all the same. The macOS keychain and a
// service that asks inside the call (KeePassXC's per-item confirmation) are
// not covered; only the caller's timeout bounds them.
func NoPrompt() {
	noPrompt.Store(true)
}
