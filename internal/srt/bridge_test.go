package srt

import (
	"bytes"
	"testing"
)

// TestBytesCloneBehavior validates that bytes.Clone (used to replace
// the custom copyBytes helper) preserves the semantics we depend on:
// independent copy, nil-safe behavior.
func TestBytesCloneBehavior(t *testing.T) {
	// bytes.Equal tests (replaces custom bytesEqual)
	eqTests := []struct {
		name string
		a, b []byte
		want bool
	}{
		{"both nil", nil, nil, true},
		{"equal", []byte{1, 2, 3}, []byte{1, 2, 3}, true},
		{"different length", []byte{1, 2}, []byte{1, 2, 3}, false},
		{"different content", []byte{1, 2, 3}, []byte{1, 2, 4}, false},
		{"empty", []byte{}, []byte{}, true},
	}
	for _, tt := range eqTests {
		t.Run("equal/"+tt.name, func(t *testing.T) {
			if got := bytes.Equal(tt.a, tt.b); got != tt.want {
				t.Errorf("bytes.Equal(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}

	// bytes.Clone tests (replaces custom copyBytes)
	t.Run("clone/independent_copy", func(t *testing.T) {
		original := []byte{1, 2, 3, 4, 5}
		copied := bytes.Clone(original)
		if !bytes.Equal(original, copied) {
			t.Error("clone should be equal to original")
		}
		copied[0] = 99
		if original[0] == 99 {
			t.Error("modifying clone should not affect original")
		}
	})

	t.Run("clone/nil_input", func(t *testing.T) {
		if bytes.Clone(nil) != nil {
			t.Error("bytes.Clone(nil) should return nil")
		}
	})
}
