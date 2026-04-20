package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// APIResolver resolves stream keys to storage targets via an HTTP API.
// Responses are cached with a configurable TTL to reduce API calls.
type APIResolver struct {
	url      string
	timeout  time.Duration
	cacheTTL time.Duration
	auth     string
	client   *http.Client
	logger   *slog.Logger

	mu    sync.RWMutex
	cache map[string]*cacheEntry
}

type cacheEntry struct {
	target    *StorageTarget
	expiresAt time.Time
}

// APIResolveResponse is the expected JSON response from the tenant resolution API.
type APIResolveResponse struct {
	StorageAccount      string `json:"storage_account"`
	Container           string `json:"container"`
	Credential          string `json:"credential"`
	ConnectionStringEnv string `json:"connection_string_env"`
	PathPrefix          string `json:"path_prefix"`
}

// NewAPIResolver creates an HTTP API-based tenant resolver.
func NewAPIResolver(cfg *APIFallbackConfig, logger *slog.Logger) *APIResolver {
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		timeout = 5 * time.Second
	}

	cacheTTL, err := time.ParseDuration(cfg.CacheTTL)
	if err != nil {
		cacheTTL = 5 * time.Minute
	}

	return &APIResolver{
		url:      cfg.URL,
		timeout:  timeout,
		cacheTTL: cacheTTL,
		auth:     cfg.AuthHeader,
		client:   &http.Client{Timeout: timeout},
		logger:   logger,
		cache:    make(map[string]*cacheEntry),
	}
}

// Resolve queries the API for the storage target of a stream key.
// Results are cached for cacheTTL duration.
func (r *APIResolver) Resolve(streamKey string) (*StorageTarget, error) {
	// Check cache first
	if target := r.getFromCache(streamKey); target != nil {
		return target, nil
	}

	// Query API
	target, err := r.queryAPI(streamKey)
	if err != nil {
		return nil, err
	}

	// Cache the result
	if target != nil {
		r.putInCache(streamKey, target)
	}

	return target, nil
}

func (r *APIResolver) getFromCache(key string) *StorageTarget {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.target
}

func (r *APIResolver) putInCache(key string, target *StorageTarget) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache[key] = &cacheEntry{
		target:    target,
		expiresAt: time.Now().Add(r.cacheTTL),
	}
}

func (r *APIResolver) queryAPI(streamKey string) (*StorageTarget, error) {
	url := fmt.Sprintf("%s?stream_key=%s", r.url, streamKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("api_resolver: create request: %w", err)
	}

	if r.auth != "" {
		req.Header.Set("Authorization", r.auth)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api_resolver: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no mapping for this stream key
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api_resolver: unexpected status %d", resp.StatusCode)
	}

	var apiResp APIResolveResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("api_resolver: decode response: %w", err)
	}

	return &StorageTarget{
		StorageAccount:      apiResp.StorageAccount,
		Container:           apiResp.Container,
		Credential:          apiResp.Credential,
		ConnectionStringEnv: apiResp.ConnectionStringEnv,
		PathPrefix:          apiResp.PathPrefix,
	}, nil
}
