package authstore

import "fmt"

// httpStatusError preserves a response's status without its body, which can
// contain credential material. Callers can distinguish rejection from outages.
type httpStatusError struct {
	code int
}

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }
