package media

import "testing"

// _tFatalf is a test helper that marks itself with t.Helper() so failure
// line numbers point to the caller, not this function. Shared across all
// media package tests (video_test.go, audio_test.go).
func _tFatalf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	t.Fatalf(format, args...)
}
