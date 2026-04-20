package main

import (
	"strings"
)

// FileResolver resolves stream keys to storage targets using the local
// configuration file. It supports exact match and longest prefix match.
type FileResolver struct {
	config *Config
}

// NewFileResolver creates a file-based tenant resolver.
func NewFileResolver(cfg *Config) *FileResolver {
	return &FileResolver{config: cfg}
}

// Resolve looks up the storage target for a stream key.
// Resolution order:
//  1. Exact match (e.g., "live/mystream" matches tenant key "live/mystream")
//  2. App prefix match (e.g., "live/mystream" matches tenant key "live")
//  3. Longest prefix match (e.g., "tenant-a/cam1" matches "tenant-a")
//
// Returns nil if no match found (caller should try API or default).
func (r *FileResolver) Resolve(streamKey string) *StorageTarget {
	cfg := r.config.Get()
	if cfg == nil || len(cfg.Tenants) == 0 {
		return nil
	}

	// 1. Exact match
	if target, ok := cfg.Tenants[streamKey]; ok {
		return target
	}

	// 2. App prefix match — extract the "app" portion (before first "/")
	if idx := strings.IndexByte(streamKey, '/'); idx > 0 {
		appName := streamKey[:idx]
		if target, ok := cfg.Tenants[appName]; ok {
			return target
		}
	}

	// 3. Longest prefix match (supports hierarchical keys like "org/team/stream")
	var bestMatch *StorageTarget
	bestLen := 0
	for key, target := range cfg.Tenants {
		if strings.HasPrefix(streamKey, key) && len(key) > bestLen {
			bestMatch = target
			bestLen = len(key)
		}
	}

	return bestMatch
}
