// auth_test.go – tests for the Validator interface, Request struct, and
// sentinel error values.
package auth

import (
	"errors"
	"testing"
)

// TestSentinelErrors verifies that each sentinel error has a non-empty
// message and can be matched with errors.Is.
func TestSentinelErrors(t *testing.T) {
	sentinels := []error{ErrUnauthorized, ErrTokenMissing, ErrForbidden}
	for _, e := range sentinels {
		if e.Error() == "" {
			t.Fatalf("sentinel error has empty message: %v", e)
		}
		if !errors.Is(e, e) {
			t.Fatalf("errors.Is failed for %v", e)
		}
	}
}

// TestRequestZeroValue ensures a zero-value Request is safe to use
// (no nil map panics).
func TestRequestZeroValue(t *testing.T) {
	var r Request
	if r.App != "" || r.StreamName != "" || r.StreamKey != "" {
		t.Fatal("zero-value Request should have empty strings")
	}
	// QueryParams is nil in zero value, but validators must handle this.
	if r.QueryParams != nil {
		t.Fatal("zero-value QueryParams should be nil")
	}
}
