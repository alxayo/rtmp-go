// segment_naming_test.go – tests for the segment filename pattern expander.
//
// The SegmentNamer takes an FFmpeg-inspired pattern string and expands it
// into concrete filenames for each recording segment. Tests cover placeholder
// expansion, zero-padding, time placeholders, literal escapes, subdirectory
// creation, counter incrementing, invalid patterns, and stream key sanitisation.
//
// Key techniques:
//   - Table-driven tests for comprehensive placeholder coverage.
//   - Injected timeFunc to make time-dependent tests deterministic.
//   - t.TempDir() for filesystem tests (auto-cleaned by the test framework).
package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedTime returns a function that always yields the same timestamp.
// Used to make time-placeholder tests deterministic.
func fixedTime(year int, month time.Month, day, hour, min, sec int) func() time.Time {
	t := time.Date(year, month, day, hour, min, sec, 0, time.UTC)
	return func() time.Time { return t }
}

// TestSegmentNamer_BasicStreamKeyAndCounter verifies that %s expands to
// the sanitised stream key and %d expands to the 1-based segment number.
func TestSegmentNamer_BasicStreamKeyAndCounter(t *testing.T) {
	namer, err := NewSegmentNamer("%s_%d", "live/mystream", "recordings", ".flv")
	if err != nil {
		t.Fatalf("NewSegmentNamer: %v", err)
	}
	// Override time so time placeholders (if any) are stable.
	namer.timeFunc = fixedTime(2026, 4, 19, 13, 5, 30)

	got, err := namer.NextName()
	if err != nil {
		t.Fatalf("NextName: %v", err)
	}
	want := filepath.Join("recordings", "live_mystream_1.flv")
	if got != want {
		t.Errorf("segment 1: got %q, want %q", got, want)
	}
}

// TestSegmentNamer_ZeroPadded verifies %03d and %04d produce correctly
// zero-padded segment numbers.
func TestSegmentNamer_ZeroPadded(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string // expected filename for segment 1
	}{
		{"pad3", "seg_%03d", filepath.Join("out", "seg_001.mp4")},
		{"pad4", "seg_%04d", filepath.Join("out", "seg_0001.mp4")},
		{"pad1", "seg_%1d", filepath.Join("out", "seg_1.mp4")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namer, err := NewSegmentNamer(tt.pattern, "unused", "out", ".mp4")
			if err != nil {
				t.Fatalf("NewSegmentNamer: %v", err)
			}
			namer.timeFunc = fixedTime(2026, 1, 1, 0, 0, 0)
			got, err := namer.NextName()
			if err != nil {
				t.Fatalf("NextName: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSegmentNamer_TimePlaceholders verifies individual time placeholders
// expand to the correct values for a fixed point in time.
func TestSegmentNamer_TimePlaceholders(t *testing.T) {
	// Fixed time: 2026-04-19 13:05:30 UTC
	tf := fixedTime(2026, 4, 19, 13, 5, 30)

	tests := []struct {
		name    string
		pattern string
		want    string // expected segment 1 filename (no baseDir, .flv extension)
	}{
		{"year", "%Y", "2026.flv"},
		{"month", "%m", "04.flv"},
		{"day", "%D", "19.flv"},
		{"hour", "%H", "13.flv"},
		{"minute", "%M", "05.flv"},
		{"second", "%S", "30.flv"},
		{"timestamp", "%T", "20260419_130530.flv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namer, err := NewSegmentNamer(tt.pattern, "s", "", ".flv")
			if err != nil {
				t.Fatalf("NewSegmentNamer: %v", err)
			}
			namer.timeFunc = tf
			got, err := namer.NextName()
			if err != nil {
				t.Fatalf("NextName: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSegmentNamer_Combined exercises a pattern with all placeholder types
// mixed together.
func TestSegmentNamer_Combined(t *testing.T) {
	namer, err := NewSegmentNamer("%s_%Y%m%D_%H%M%S_%03d", "live/cam1", "rec", ".mp4")
	if err != nil {
		t.Fatalf("NewSegmentNamer: %v", err)
	}
	namer.timeFunc = fixedTime(2026, 4, 19, 13, 5, 30)

	got, err := namer.NextName()
	if err != nil {
		t.Fatalf("NextName: %v", err)
	}
	want := filepath.Join("rec", "live_cam1_20260419_130530_001.mp4")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSegmentNamer_LiteralPercent verifies that %% is expanded to a
// single literal '%' character.
func TestSegmentNamer_LiteralPercent(t *testing.T) {
	namer, err := NewSegmentNamer("100%%_done_%d", "s", "", ".txt")
	if err != nil {
		t.Fatalf("NewSegmentNamer: %v", err)
	}
	namer.timeFunc = fixedTime(2026, 1, 1, 0, 0, 0)

	got, err := namer.NextName()
	if err != nil {
		t.Fatalf("NextName: %v", err)
	}
	want := "100%_done_1.txt"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSegmentNamer_SubdirectoryCreation verifies that patterns containing
// '/' cause subdirectories to be created automatically.
func TestSegmentNamer_SubdirectoryCreation(t *testing.T) {
	baseDir := t.TempDir()
	namer, err := NewSegmentNamer("%Y/%m/%D/%s_%03d", "live/cam", baseDir, ".flv")
	if err != nil {
		t.Fatalf("NewSegmentNamer: %v", err)
	}
	namer.timeFunc = fixedTime(2026, 4, 19, 13, 5, 30)

	got, err := namer.NextName()
	if err != nil {
		t.Fatalf("NextName: %v", err)
	}

	// Verify the path looks correct.
	want := filepath.Join(baseDir, "2026", "04", "19", "live_cam_001.flv")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Verify the directory was actually created on disk.
	dir := filepath.Dir(got)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected %q to be a directory", dir)
	}
}

// TestSegmentNamer_IncrementingCounter calls NextName() multiple times and
// verifies the segment counter increments from 1 upward.
func TestSegmentNamer_IncrementingCounter(t *testing.T) {
	namer, err := NewSegmentNamer("seg_%d", "s", "", ".flv")
	if err != nil {
		t.Fatalf("NewSegmentNamer: %v", err)
	}
	namer.timeFunc = fixedTime(2026, 1, 1, 0, 0, 0)

	expected := []string{"seg_1.flv", "seg_2.flv", "seg_3.flv", "seg_4.flv", "seg_5.flv"}
	for i, want := range expected {
		got, err := namer.NextName()
		if err != nil {
			t.Fatalf("NextName call %d: %v", i+1, err)
		}
		if got != want {
			t.Errorf("call %d: got %q, want %q", i+1, got, want)
		}
	}
}

// TestSegmentNamer_InvalidPatterns verifies that invalid placeholders are
// caught during construction and that ValidatePattern returns clear errors.
func TestSegmentNamer_InvalidPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantSub string // substring expected in the error message
	}{
		{"unknown_z", "%z", "unknown placeholder '%z'"},
		{"unknown_q", "%q", "unknown placeholder '%q'"},
		{"trailing_percent", "hello%", "trailing '%'"},
		{"digits_no_d", "%03x", "must be followed by 'd'"},
		{"digits_end", "%03", "must be followed by 'd'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate via ValidatePattern directly.
			err := ValidatePattern(tt.pattern)
			if err == nil {
				t.Fatalf("ValidatePattern(%q): expected error, got nil", tt.pattern)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("ValidatePattern(%q): error %q does not contain %q",
					tt.pattern, err.Error(), tt.wantSub)
			}

			// Also verify the constructor rejects it.
			_, cerr := NewSegmentNamer(tt.pattern, "s", "", ".flv")
			if cerr == nil {
				t.Errorf("NewSegmentNamer(%q): expected error, got nil", tt.pattern)
			}
		})
	}
}

// TestSegmentNamer_EmptyStreamKey ensures an empty stream key does not
// cause errors and simply produces an empty expansion for %s.
func TestSegmentNamer_EmptyStreamKey(t *testing.T) {
	namer, err := NewSegmentNamer("prefix_%s_%d", "", "", ".flv")
	if err != nil {
		t.Fatalf("NewSegmentNamer: %v", err)
	}
	namer.timeFunc = fixedTime(2026, 1, 1, 0, 0, 0)

	got, err := namer.NextName()
	if err != nil {
		t.Fatalf("NextName: %v", err)
	}
	want := "prefix__1.flv"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSegmentNamer_StreamKeySlashesToUnderscores verifies that forward
// slashes in the stream key are replaced with underscores.
func TestSegmentNamer_StreamKeySlashesToUnderscores(t *testing.T) {
	tests := []struct {
		name      string
		streamKey string
		want      string // expected %s expansion
	}{
		{"single_slash", "live/stream", "live_stream"},
		{"multi_slash", "a/b/c/d", "a_b_c_d"},
		{"no_slash", "plainkey", "plainkey"},
		{"trailing_slash", "live/stream/", "live_stream_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namer, err := NewSegmentNamer("%s", tt.streamKey, "", ".flv")
			if err != nil {
				t.Fatalf("NewSegmentNamer: %v", err)
			}
			namer.timeFunc = fixedTime(2026, 1, 1, 0, 0, 0)
			got, err := namer.NextName()
			if err != nil {
				t.Fatalf("NextName: %v", err)
			}
			// Strip the extension to isolate the stream key expansion.
			got = strings.TrimSuffix(got, ".flv")
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSegmentNamer_TimePlaceholders_Live verifies that when using the real
// clock (no fixedTime override), time placeholders produce plausible values.
// We check that %Y is a 4-digit year and %m is a 2-digit month rather than
// exact values, since the real time changes.
func TestSegmentNamer_TimePlaceholders_Live(t *testing.T) {
	namer, err := NewSegmentNamer("%Y_%m_%D_%H_%M_%S", "s", "", ".flv")
	if err != nil {
		t.Fatalf("NewSegmentNamer: %v", err)
	}

	got, err := namer.NextName()
	if err != nil {
		t.Fatalf("NextName: %v", err)
	}
	got = strings.TrimSuffix(got, ".flv")

	// Split on underscore; expect 6 parts: year, month, day, hour, minute, second.
	parts := strings.Split(got, "_")
	if len(parts) != 6 {
		t.Fatalf("expected 6 parts, got %d: %q", len(parts), got)
	}

	// Year should be 4 digits, all others 2 digits.
	if len(parts[0]) != 4 {
		t.Errorf("year %q should be 4 digits", parts[0])
	}
	for i := 1; i < 6; i++ {
		if len(parts[i]) != 2 {
			t.Errorf("part[%d] %q should be 2 digits", i, parts[i])
		}
	}
}

// TestValidatePattern_ValidPatterns ensures that well-formed patterns pass
// validation without error.
func TestValidatePattern_ValidPatterns(t *testing.T) {
	valids := []string{
		"%s_%d",
		"%s_%03d",
		"%Y/%m/%D/%s_%04d",
		"literal_only",
		"100%%_done",
		"%T_%s_%d",
		"%Y%m%D_%H%M%S",
		"",
	}
	for _, p := range valids {
		if err := ValidatePattern(p); err != nil {
			t.Errorf("ValidatePattern(%q): unexpected error: %v", p, err)
		}
	}
}
