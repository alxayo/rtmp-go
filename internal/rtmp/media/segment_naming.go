package media

// Segment Filename Pattern Expander
// ----------------------------------
// Generates filenames for segmented recordings using an FFmpeg-inspired
// pattern string. Each call to NextName() returns the next segment path
// with all placeholders expanded.
//
// Supported placeholders:
//   %s        – stream key (slashes replaced with underscores)
//   %d        – segment number (1-based)
//   %03d, etc – zero-padded segment number (width 1–9)
//   %Y        – year (4-digit)
//   %m        – month (2-digit, zero-padded)
//   %D        – day of month (2-digit)
//   %H        – hour (24h, 2-digit)
//   %M        – minute (2-digit)
//   %S        – second (2-digit)
//   %T        – full timestamp YYYYMMDD_HHMMSS
//   %%        – literal percent sign
//
// Example:
//   pattern: "%s/%Y%m%D_%03d"  baseDir: "recordings"  ext: ".flv"
//   → "recordings/live_mystream/20260419_001.flv"

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SegmentNamer generates filenames for recording segments using an
// FFmpeg-inspired pattern. Each call to NextName() returns the next
// segment filename with placeholders expanded.
type SegmentNamer struct {
	pattern   string // the user's pattern template (e.g., "%s_%03d")
	streamKey string // stream key for %s expansion (slashes → underscores)
	baseDir   string // base directory prepended to generated paths
	extension string // file extension including the dot (e.g., ".flv")
	counter   int    // current segment number (incremented each call)

	// timeFunc returns the current time. It defaults to time.Now but can
	// be overridden in tests to produce deterministic output.
	timeFunc func() time.Time
}

// NewSegmentNamer creates a namer from a pattern, stream key, base directory,
// and file extension. The extension should include the dot (e.g., ".mp4").
// The pattern is validated — returns an error if it contains invalid placeholders.
func NewSegmentNamer(pattern, streamKey, baseDir, extension string) (*SegmentNamer, error) {
	// Validate the pattern before constructing the namer so callers get
	// early feedback about typos in config files.
	if err := ValidatePattern(pattern); err != nil {
		return nil, err
	}

	return &SegmentNamer{
		pattern:   pattern,
		streamKey: sanitizeStreamKey(streamKey),
		baseDir:   baseDir,
		extension: extension,
		counter:   0, // NextName increments before use → first segment is 1
		timeFunc:  time.Now,
	}, nil
}

// NextName returns the next segment filename (full path including base dir
// and extension). Increments the segment counter. Time placeholders are
// evaluated at the moment of the call.
// Also creates any subdirectories if the expanded pattern contains '/'.
func (n *SegmentNamer) NextName() (string, error) {
	// Advance to the next segment number (1-based).
	n.counter++

	// Capture "now" once so all time placeholders within a single call
	// are consistent (no minute rollover mid-expansion).
	now := n.timeFunc()

	// Expand all placeholders in the pattern string.
	expanded, err := expandPattern(n.pattern, n.streamKey, n.counter, now)
	if err != nil {
		return "", fmt.Errorf("segment_naming.expand: %w", err)
	}

	// Build the full path: baseDir/expanded.extension
	fullPath := filepath.Join(n.baseDir, expanded+n.extension)

	// If the expanded path contains subdirectories, ensure they exist.
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("segment_naming.mkdir: %w", err)
	}

	return fullPath, nil
}

// ValidatePattern checks if a pattern string is valid without creating a namer.
// It walks the pattern looking for '%' sequences and verifies each one is a
// recognised placeholder. Returns a descriptive error if invalid.
func ValidatePattern(pattern string) error {
	i := 0
	for i < len(pattern) {
		// Fast-skip ordinary characters until we hit a '%'.
		if pattern[i] != '%' {
			i++
			continue
		}

		// We found '%'. Consume the placeholder.
		i++ // move past '%'
		if i >= len(pattern) {
			return fmt.Errorf("segment_naming.validate: trailing '%%' at end of pattern")
		}

		ch := pattern[i]

		// Literal '%%' escape — always valid.
		if ch == '%' {
			i++
			continue
		}

		// Check for zero-padded segment number: %<width>d where width is
		// one or more digits (e.g., %03d, %4d). We accept widths 1–9 digits
		// wide, though in practice 1–4 is common.
		if ch >= '0' && ch <= '9' {
			// Consume all digits forming the width specifier.
			for i < len(pattern) && pattern[i] >= '0' && pattern[i] <= '9' {
				i++
			}
			// The digits must be followed by 'd' to be a valid padded number.
			if i >= len(pattern) || pattern[i] != 'd' {
				return fmt.Errorf("segment_naming.validate: invalid placeholder '%%%s' — "+
					"numeric width must be followed by 'd' (e.g., %%03d)", pattern[i-1:i])
			}
			i++ // consume 'd'
			continue
		}

		// Single-character placeholders.
		switch ch {
		case 's', 'd', 'Y', 'm', 'D', 'H', 'M', 'S', 'T':
			i++
		default:
			return fmt.Errorf("segment_naming.validate: unknown placeholder '%%%c' — "+
				"valid: %%s %%d %%03d %%Y %%m %%D %%H %%M %%S %%T %%%%", ch)
		}
	}
	return nil
}

// expandPattern replaces all placeholders in pattern with their concrete
// values. It processes the string byte-by-byte, copying literals and
// expanding '%'-prefixed sequences.
func expandPattern(pattern, streamKey string, segNum int, now time.Time) (string, error) {
	var b strings.Builder
	// Pre-allocate a reasonable capacity to avoid repeated growth.
	b.Grow(len(pattern) + 32)

	i := 0
	for i < len(pattern) {
		// Copy ordinary characters verbatim.
		if pattern[i] != '%' {
			b.WriteByte(pattern[i])
			i++
			continue
		}

		// '%' found — determine which placeholder follows.
		i++ // skip '%'
		if i >= len(pattern) {
			return "", fmt.Errorf("trailing '%%' at end of pattern")
		}

		ch := pattern[i]

		// Literal '%%' → single '%'.
		if ch == '%' {
			b.WriteByte('%')
			i++
			continue
		}

		// Zero-padded segment number: %<width>d  (e.g., %03d, %4d).
		if ch >= '0' && ch <= '9' {
			// Collect all the width digits.
			widthStart := i
			for i < len(pattern) && pattern[i] >= '0' && pattern[i] <= '9' {
				i++
			}
			// Parse the width (already validated, so Atoi won't fail).
			width := 0
			for _, c := range pattern[widthStart:i] {
				width = width*10 + int(c-'0')
			}
			i++ // skip the trailing 'd' (guaranteed by validation)
			// Format the segment number with the requested zero-padding.
			b.WriteString(fmt.Sprintf("%0*d", width, segNum))
			continue
		}

		// Single-character placeholders.
		switch ch {
		case 's':
			b.WriteString(streamKey)
		case 'd':
			b.WriteString(fmt.Sprintf("%d", segNum))
		case 'Y':
			b.WriteString(fmt.Sprintf("%04d", now.Year()))
		case 'm':
			b.WriteString(fmt.Sprintf("%02d", now.Month()))
		case 'D':
			b.WriteString(fmt.Sprintf("%02d", now.Day()))
		case 'H':
			b.WriteString(fmt.Sprintf("%02d", now.Hour()))
		case 'M':
			b.WriteString(fmt.Sprintf("%02d", now.Minute()))
		case 'S':
			b.WriteString(fmt.Sprintf("%02d", now.Second()))
		case 'T':
			b.WriteString(now.Format("20060102_150405"))
		}
		i++
	}

	return b.String(), nil
}

// sanitizeStreamKey replaces forward slashes with underscores so the stream
// key is safe for use as a filesystem path component.
func sanitizeStreamKey(key string) string {
	return strings.ReplaceAll(key, "/", "_")
}
