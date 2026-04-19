package srt

import (
	"bytes"
	"log/slog"
	"os"
	"testing"
)

// testLogger returns a discarding logger for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

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

// ─── Container detection tests ──────────────────────────────────────────────

// TestDetectContainer validates that the bridge correctly identifies
// MPEG-TS and Matroska containers from the first bytes of data.
func TestDetectContainer(t *testing.T) {
	newTestBridge := func() *Bridge {
		return &Bridge{
			log: testLogger(),
		}
	}

	t.Run("detects Matroska from EBML header", func(t *testing.T) {
		b := newTestBridge()

		// Matroska files start with the EBML element ID: 0x1A45DFA3
		data := make([]byte, 200)
		copy(data[:4], []byte{0x1A, 0x45, 0xDF, 0xA3})

		detected := b.detectContainer(data)
		if !detected {
			t.Fatal("expected detection to succeed")
		}
		if b.container != containerMKV {
			t.Errorf("container = %d, want containerMKV (%d)", b.container, containerMKV)
		}
		if b.mkvDemuxer == nil {
			t.Error("mkvDemuxer should not be nil after MKV detection")
		}
		if b.demuxer != nil {
			t.Error("TS demuxer should be nil when MKV is detected")
		}
	})

	t.Run("detects MKV with only 4 bytes", func(t *testing.T) {
		b := newTestBridge()

		// Only 4 bytes available — enough for MKV detection
		data := []byte{0x1A, 0x45, 0xDF, 0xA3}

		detected := b.detectContainer(data)
		if !detected {
			t.Fatal("expected MKV detection with just 4 bytes")
		}
		if b.container != containerMKV {
			t.Errorf("container = %d, want containerMKV", b.container)
		}
	})

	t.Run("detects MPEG-TS from dual sync bytes", func(t *testing.T) {
		b := newTestBridge()

		// MPEG-TS: 0x47 at offset 0 and offset 188
		data := make([]byte, 200)
		data[0] = 0x47
		data[188] = 0x47

		detected := b.detectContainer(data)
		if !detected {
			t.Fatal("expected detection to succeed")
		}
		if b.container != containerTS {
			t.Errorf("container = %d, want containerTS (%d)", b.container, containerTS)
		}
		if b.demuxer == nil {
			t.Error("TS demuxer should not be nil after TS detection")
		}
		if b.mkvDemuxer != nil {
			t.Error("MKV demuxer should be nil when TS is detected")
		}
	})

	t.Run("detects MPEG-TS with single sync byte fallback", func(t *testing.T) {
		b := newTestBridge()

		// Only first byte is 0x47, second position is not
		data := make([]byte, 200)
		data[0] = 0x47
		data[188] = 0x00

		detected := b.detectContainer(data)
		if !detected {
			t.Fatal("expected TS detection with single sync byte")
		}
		if b.container != containerTS {
			t.Errorf("container = %d, want containerTS", b.container)
		}
	})

	t.Run("needs more data when buffer too short for TS", func(t *testing.T) {
		b := newTestBridge()

		// Only 100 bytes — not enough for TS detection (need 189)
		data := make([]byte, 100)
		data[0] = 0x47

		detected := b.detectContainer(data)
		if detected {
			t.Fatal("should not detect with only 100 bytes (not MKV magic)")
		}
		if b.container != containerUnknown {
			t.Errorf("container should still be unknown")
		}
	})

	t.Run("unknown format defaults to TS", func(t *testing.T) {
		b := newTestBridge()

		// 200 bytes of garbage — neither TS sync nor MKV magic
		data := make([]byte, 200)
		data[0] = 0xAA

		detected := b.detectContainer(data)
		if !detected {
			t.Fatal("should detect (fallback to TS) with enough data")
		}
		if b.container != containerTS {
			t.Errorf("container = %d, want containerTS (fallback)", b.container)
		}
	})

	t.Run("MKV magic takes priority with enough data", func(t *testing.T) {
		b := newTestBridge()

		// MKV magic at start, even though 0x47 appears at 188
		data := make([]byte, 200)
		copy(data[:4], []byte{0x1A, 0x45, 0xDF, 0xA3})
		data[188] = 0x47

		detected := b.detectContainer(data)
		if !detected {
			t.Fatal("expected detection to succeed")
		}
		if b.container != containerMKV {
			t.Errorf("MKV magic should take priority, got container=%d", b.container)
		}
	})
}

// TestMKVMagicBytes verifies the expected magic byte values.
func TestMKVMagicBytes(t *testing.T) {
	expected := []byte{0x1A, 0x45, 0xDF, 0xA3}
	if !bytes.Equal(mkvMagic, expected) {
		t.Errorf("mkvMagic = %x, want %x", mkvMagic, expected)
	}
}
