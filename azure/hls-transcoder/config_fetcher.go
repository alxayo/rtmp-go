package main

// Config Fetcher — Fetches stream configuration from the Platform API
// ====================================================================
// This component handles two types of config fetches:
//
// A. System defaults (fetched at startup, refreshed periodically):
//    GET {platformURL}/api/internal/stream-config/defaults
//    Cached in memory — provides fallback when per-event fetches fail.
//
// B. Per-event config (fetched on each publish_start):
//    GET {platformURL}/api/internal/events/{eventId}/stream-config
//    Includes merged config (system defaults + event overrides).
//
// The four-tier fallback chain (in order of preference):
//   1. Fresh per-event config from API (200 OK)
//   2. Cached per-event config within TTL (on timeout/5xx)
//   3. Cached system defaults (on timeout/5xx with no event cache)
//   4. Hardcoded Go constants (before first successful system defaults fetch)
//
// Non-retriable errors (404, 403) return an error — caller must NOT start FFmpeg.
//
// See latency-controls-plan.md §4.1 for the full specification.

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ConfigFetcherConfig holds the settings for creating a ConfigFetcher.
// Populated from CLI flags in main.go.
type ConfigFetcherConfig struct {
	PlatformURL  string        // Base URL of the Platform API (e.g., "http://platform:3000")
	APIKey       string        // X-Internal-Api-Key value for authentication
	CacheTTL     time.Duration // How long cached configs are valid (default: 10m)
	FetchTimeout time.Duration // HTTP timeout for each fetch request (default: 2s)
}

// cachedConfig stores a fetched config alongside its fetch timestamp.
// Used for both per-event cache entries and the system defaults cache.
type cachedConfig struct {
	config    *StreamConfigResponse
	fetchedAt time.Time
}

// ConfigFetcher manages config retrieval from the Platform API with caching.
// It is created once in main.go and passed to the Transcoder via NewTranscoder().
// The fetcher is process-lifetime scoped — it is NOT recreated per stream.
type ConfigFetcher struct {
	cfg    ConfigFetcherConfig
	logger *slog.Logger
	client *http.Client

	// System defaults cache — refreshed periodically in background.
	// Protected by its own mutex so it doesn't block per-event fetches.
	systemMu       sync.RWMutex
	systemDefaults *SystemDefaultsResponse
	systemFetchedAt time.Time

	// Per-event config cache — keyed by event ID.
	// Each entry has a TTL; stale entries are used as fallback on API errors.
	eventMu    sync.RWMutex
	eventCache map[string]*cachedConfig

	// stopCh is closed to signal the background refresh goroutine to stop.
	stopCh chan struct{}
}

// NewConfigFetcher creates a new config fetcher and starts the background
// system defaults refresh loop.
func NewConfigFetcher(cfg ConfigFetcherConfig, logger *slog.Logger) *ConfigFetcher {
	cf := &ConfigFetcher{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: cfg.FetchTimeout},
		eventCache: make(map[string]*cachedConfig),
		stopCh:     make(chan struct{}),
	}

	// Fetch system defaults immediately at startup (best-effort).
	// If this fails, we fall back to hardcoded Go constants.
	if err := cf.refreshSystemDefaults(); err != nil {
		logger.Warn("initial system defaults fetch failed, using hardcoded defaults",
			"error", err,
		)
	}

	// Start background refresh loop for system defaults
	go cf.systemDefaultsRefreshLoop()

	return cf
}

// Stop signals the background refresh loop to exit.
// Called during graceful shutdown.
func (cf *ConfigFetcher) Stop() {
	close(cf.stopCh)
}

// systemDefaultsRefreshLoop periodically refreshes the cached system defaults.
// Runs until Stop() is called.
func (cf *ConfigFetcher) systemDefaultsRefreshLoop() {
	ticker := time.NewTicker(cf.cfg.CacheTTL)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := cf.refreshSystemDefaults(); err != nil {
				cf.logger.Warn("system defaults refresh failed, keeping previous cache",
					"error", err,
				)
			}
		case <-cf.stopCh:
			return
		}
	}
}

// refreshSystemDefaults fetches system defaults from the Platform API.
func (cf *ConfigFetcher) refreshSystemDefaults() error {
	url := strings.TrimSuffix(cf.cfg.PlatformURL, "/") + "/api/internal/stream-config/defaults"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Internal-Api-Key", cf.cfg.APIKey)

	resp, err := cf.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch system defaults: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("system defaults returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var defaults SystemDefaultsResponse
	if err := json.Unmarshal(body, &defaults); err != nil {
		return fmt.Errorf("parse system defaults: %w", err)
	}

	cf.systemMu.Lock()
	cf.systemDefaults = &defaults
	cf.systemFetchedAt = time.Now()
	cf.systemMu.Unlock()

	cf.logger.Info("system defaults refreshed",
		"profile", defaults.Transcoder.Profile,
		"hls_time", defaults.Transcoder.HLSTime,
	)

	return nil
}

// getSystemDefaults returns the cached system defaults, or hardcoded defaults
// if no successful fetch has happened yet.
func (cf *ConfigFetcher) getSystemDefaults() EventTranscoderConfig {
	cf.systemMu.RLock()
	defer cf.systemMu.RUnlock()

	if cf.systemDefaults != nil {
		return cf.systemDefaults.Transcoder
	}
	return DefaultEventTranscoderConfig
}

// FetchEventConfig fetches the stream configuration for a specific event.
//
// This is called by Transcoder.Start() on each publish_start, OUTSIDE the
// process-registry lock (so network latency doesn't serialize stream starts).
//
// Returns the event config and the config source for logging.
// Returns an error for non-retriable failures (404, 403) — caller must NOT start FFmpeg.
func (cf *ConfigFetcher) FetchEventConfig(eventID string) (*StreamConfigResponse, string, error) {
	url := strings.TrimSuffix(cf.cfg.PlatformURL, "/") + "/api/internal/events/" + eventID + "/stream-config"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Internal-Api-Key", cf.cfg.APIKey)

	resp, err := cf.client.Do(req)
	if err != nil {
		// Network error or timeout — try fallback chain
		return cf.fallback(eventID, fmt.Errorf("fetch event config: %w", err))
	}
	defer resp.Body.Close()

	// Non-retriable errors: transcoder must NOT start FFmpeg
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", fmt.Errorf("event %s not found or inactive (404)", eventID)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, "", fmt.Errorf("event %s auth failed (%d)", eventID, resp.StatusCode)
	}

	// Server error — try fallback chain
	if resp.StatusCode >= 500 {
		return cf.fallback(eventID, fmt.Errorf("event config returned status %d", resp.StatusCode))
	}

	// Success — parse and cache
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return cf.fallback(eventID, fmt.Errorf("read event config: %w", err))
	}

	var config StreamConfigResponse
	if err := json.Unmarshal(body, &config); err != nil {
		// Malformed response — fall back to system defaults (event is valid, config is broken)
		cf.logger.Warn("malformed event config response, falling back to system defaults",
			"event_id", eventID,
			"error", err,
		)
		return cf.fallback(eventID, fmt.Errorf("parse event config: %w", err))
	}

	// Update the per-event cache
	cf.eventMu.Lock()
	cf.eventCache[eventID] = &cachedConfig{
		config:    &config,
		fetchedAt: time.Now(),
	}
	cf.eventMu.Unlock()

	return &config, "event", nil
}

// fallback implements the fallback chain when a per-event fetch fails:
//   Tier 2: cached per-event config within TTL
//   Tier 3: cached system defaults
//   Tier 4: hardcoded Go constants
func (cf *ConfigFetcher) fallback(eventID string, fetchErr error) (*StreamConfigResponse, string, error) {
	// Tier 2: Check per-event cache
	cf.eventMu.RLock()
	cached, exists := cf.eventCache[eventID]
	cf.eventMu.RUnlock()

	if exists && time.Since(cached.fetchedAt) < cf.cfg.CacheTTL {
		cf.logger.Warn("using cached event config due to fetch failure",
			"event_id", eventID,
			"error", fetchErr,
			"cache_age", time.Since(cached.fetchedAt).Round(time.Second),
		)
		return cached.config, "event-cache", nil
	}

	// Tier 3: Use system defaults
	cf.systemMu.RLock()
	hasSystemDefaults := cf.systemDefaults != nil
	cf.systemMu.RUnlock()

	if hasSystemDefaults {
		cf.logger.Warn("using system defaults due to fetch failure",
			"event_id", eventID,
			"error", fetchErr,
		)
		defaults := cf.getSystemDefaults()
		return &StreamConfigResponse{
			EventID:      eventID,
			EventActive:  true,
			ConfigSource: "system-default",
			Transcoder:   defaults,
		}, "system-default", nil
	}

	// Tier 4: Hardcoded defaults (before first system defaults fetch succeeds)
	cf.logger.Warn("using hardcoded defaults — no system defaults available yet",
		"event_id", eventID,
		"error", fetchErr,
	)
	return &StreamConfigResponse{
		EventID:      eventID,
		EventActive:  true,
		ConfigSource: "system-default",
		Transcoder:   DefaultEventTranscoderConfig,
	}, "hardcoded", nil
}

// extractEventID extracts the event ID from an RTMP stream key.
//
// Stream key format: "live/{eventId}" where eventId is a UUID.
// If the key contains no "/", the entire key is used as the event ID.
// Returns an error if the extracted ID is empty.
func extractEventID(streamKey string) (string, error) {
	parts := strings.Split(streamKey, "/")
	eventID := parts[len(parts)-1]
	if eventID == "" {
		return "", fmt.Errorf("empty event ID extracted from stream key: %q", streamKey)
	}
	return eventID, nil
}
