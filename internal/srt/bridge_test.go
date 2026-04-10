package srt

import (
	"testing"
)

// TestBytesEqual tests the byte slice comparison helper.
func TestBytesEqual(t *testing.T) {
	tests := []struct {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bytesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("bytesEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestCopyBytes tests the byte slice copy helper.
func TestCopyBytes(t *testing.T) {
	original := []byte{1, 2, 3, 4, 5}
	copied := copyBytes(original)

	// Should be equal
	if !bytesEqual(original, copied) {
		t.Error("copy should be equal to original")
	}

	// Should be a different underlying array
	copied[0] = 99
	if original[0] == 99 {
		t.Error("modifying copy should not affect original")
	}

	// Nil input → nil output
	if copyBytes(nil) != nil {
		t.Error("copyBytes(nil) should return nil")
	}
}
