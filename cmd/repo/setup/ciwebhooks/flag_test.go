package ciwebhooks

import "testing"

func TestValidatePreservesNonemptySecret(t *testing.T) {
	// Spaces are valid HMAC key bytes. Validation must not trim or otherwise
	// change a nonempty secret while rejecting the empty value.
	secret := "  synthetic shared secret  "
	f := &flag{WebhookSharedSecret: secret}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	if f.WebhookSharedSecret != secret {
		t.Fatal("validation changed the shared secret")
	}
}
