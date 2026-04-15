package srt

import (
	"strings"
	"testing"
)

// TestValidatePassphraseLength verifies that Config.Validate enforces the
// SRT spec requirement that passphrases be 10-79 characters.
func TestValidatePassphraseLength(t *testing.T) {
	tests := []struct {
		name       string
		passphrase string
		wantErr    string // substring expected in error, empty = no error
	}{
		{"empty passphrase is valid", "", ""},
		{"too short (5 chars)", "short", "too short"},
		{"too short (9 chars)", "ninechars", "too short"},
		{"minimum valid (10 chars)", "tencharsPP", ""},
		{"maximum valid (79 chars)", strings.Repeat("a", 79), ""},
		{"too long (80 chars)", strings.Repeat("a", 80), "too long"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Passphrase: tc.passphrase, PbKeyLen: 16}
			err := cfg.Validate()

			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tc.wantErr)
				}
			}
		})
	}
}

// TestValidatePbKeyLen verifies that Config.Validate rejects invalid AES key sizes.
func TestValidatePbKeyLen(t *testing.T) {
	tests := []struct {
		name     string
		pbKeyLen int
		wantErr  bool
	}{
		{"zero is valid", 0, false},
		{"AES-128", 16, false},
		{"AES-192", 24, false},
		{"AES-256", 32, false},
		{"invalid 15", 15, true},
		{"invalid 64", 64, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{PbKeyLen: tc.pbKeyLen}
			err := cfg.Validate()

			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
