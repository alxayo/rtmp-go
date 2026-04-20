package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
)

// Router extracts stream keys from segment file paths and resolves them
// to tenant storage targets.
type Router struct {
	fileResolver *FileResolver
	apiResolver  *APIResolver
	config       *Config
	logger       *slog.Logger
}

// NewRouter creates a router with the given resolvers.
func NewRouter(file *FileResolver, api *APIResolver, cfg *Config, logger *slog.Logger) *Router {
	return &Router{
		fileResolver: file,
		apiResolver:  api,
		config:       cfg,
		logger:       logger,
	}
}

// Resolve determines the storage target for a segment file.
// Resolution order: file config (exact/prefix match) → API fallback → default.
func (r *Router) Resolve(path string) (*StorageTarget, error) {
	streamKey := r.ExtractStreamKey(path)
	if streamKey == "" {
		return nil, fmt.Errorf("router: could not extract stream key from path: %s", path)
	}

	// Try file-based resolution first
	if target := r.fileResolver.Resolve(streamKey); target != nil {
		r.logger.Debug("resolved via config file", "stream_key", streamKey, "account", target.StorageAccount)
		return target, nil
	}

	// Try API fallback
	if r.apiResolver != nil {
		target, err := r.apiResolver.Resolve(streamKey)
		if err != nil {
			r.logger.Warn("API resolver failed, trying default", "stream_key", streamKey, "error", err)
		} else if target != nil {
			r.logger.Debug("resolved via API", "stream_key", streamKey, "account", target.StorageAccount)
			return target, nil
		}
	}

	// Fall back to default
	cfg := r.config.Get()
	if cfg.Default != nil {
		r.logger.Debug("using default storage", "stream_key", streamKey)
		return cfg.Default, nil
	}

	return nil, fmt.Errorf("router: no storage target for stream key %q and no default configured", streamKey)
}

// ExtractStreamKey parses the stream key from a segment file path.
//
// Supports two layouts:
//  1. Nested: recordings/{stream_key}/timestamp_seg001.flv
//     → stream_key is the first directory component after the watch dir
//  2. Flat: recordings/{stream_key}_YYYYMMDD_HHMMSS_seg001.flv
//     → stream_key is everything before the first _YYYYMMDD_ pattern
//
// The stream key has underscores restored to slashes (rtmp-go's segment namer
// replaces "/" with "_" in sanitizeStreamKey).
func (r *Router) ExtractStreamKey(path string) string {
	// Get path relative to watch dir (handled externally, but be defensive)
	filename := filepath.Base(path)
	dir := filepath.Dir(path)

	// Strategy 1: Check if directory structure contains the stream key.
	// If the parent dir is NOT the watch root (i.e., there's a subdirectory),
	// use the subdirectory name as the stream key.
	dirBase := filepath.Base(dir)
	if dirBase != "." && dirBase != "recordings" && !isTimestamp(dirBase) {
		// The directory name is the stream key (e.g., "live_mystream")
		return restoreStreamKey(dirBase)
	}

	// Strategy 2: Parse the flat filename.
	// Pattern: {stream_key}_{YYYYMMDD}_{HHMMSS}_seg{NNN}.ext
	// or: {stream_key}_{YYYYMMDD_HHMMSS}_seg{NNN}.ext
	key := extractKeyFromFilename(filename)
	if key != "" {
		return restoreStreamKey(key)
	}

	// Fallback: use the filename without extension as a generic key
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

// timestampPattern matches the YYYYMMDD_HHMMSS pattern in segment filenames.
var timestampPattern = regexp.MustCompile(`_(\d{8}_\d{6})_seg`)

// extractKeyFromFilename extracts the stream key portion from a flat segment filename.
// Example: "live_mystream_20260419_103406_seg001.flv" → "live_mystream"
func extractKeyFromFilename(filename string) string {
	// Remove extension
	name := strings.TrimSuffix(filename, filepath.Ext(filename))

	// Find the timestamp pattern _YYYYMMDD_HHMMSS_seg
	loc := timestampPattern.FindStringIndex(name)
	if loc != nil {
		return name[:loc[0]]
	}

	// Alternative: try to find _segNNN suffix
	segPattern := regexp.MustCompile(`_seg\d+$`)
	segLoc := segPattern.FindStringIndex(name)
	if segLoc != nil {
		// Everything before _segNNN, but we need to separate the timestamp too
		prefix := name[:segLoc[0]]
		// Try to strip trailing _YYYYMMDD_HHMMSS
		tsOnly := regexp.MustCompile(`_\d{8}_\d{6}$`)
		if tsLoc := tsOnly.FindStringIndex(prefix); tsLoc != nil {
			return prefix[:tsLoc[0]]
		}
		return prefix
	}

	return ""
}

// restoreStreamKey converts the sanitized key back to the original format.
// rtmp-go replaces "/" with "_" in stream keys. We use a heuristic:
// common RTMP app names (live, app, stream) followed by underscore get "/" restored.
// For multi-component keys, the first underscore after a known app prefix becomes "/".
func restoreStreamKey(sanitized string) string {
	// Known RTMP application prefixes that typically precede a "/"
	prefixes := []string{"live", "app", "stream", "publish", "play"}

	for _, prefix := range prefixes {
		if strings.HasPrefix(sanitized, prefix+"_") {
			// Restore the first underscore as "/"
			return prefix + "/" + sanitized[len(prefix)+1:]
		}
	}

	// If no known prefix, return as-is (the tenant config can use
	// the sanitized form as a key)
	return sanitized
}

// isTimestamp checks if a string looks like a timestamp directory name (YYYYMMDD or similar).
func isTimestamp(s string) bool {
	if len(s) == 8 {
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	return false
}
