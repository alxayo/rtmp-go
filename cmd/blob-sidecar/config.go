package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// TenantConfig represents the complete tenant routing configuration.
type TenantConfig struct {
	Tenants     map[string]*StorageTarget `json:"tenants"`
	Default     *StorageTarget            `json:"default"`
	APIFallback *APIFallbackConfig        `json:"api_fallback"`
}

// StorageTarget defines where to upload segments for a given tenant.
type StorageTarget struct {
	StorageAccount      string `json:"storage_account"`       // e.g., "https://account.blob.core.windows.net"
	Container           string `json:"container"`             // blob container name
	Credential          string `json:"credential"`            // "managed-identity" or "connection-string"
	ConnectionStringEnv string `json:"connection_string_env"` // env var name holding the connection string
	PathPrefix          string `json:"path_prefix"`           // optional prefix in blob path
}

// APIFallbackConfig defines the HTTP API resolver configuration.
type APIFallbackConfig struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url"`       // e.g., "https://streamgate.example.com/api/tenants/resolve"
	Timeout  string `json:"timeout"`   // e.g., "5s"
	CacheTTL string `json:"cache_ttl"` // e.g., "5m"
	// Optional auth header
	AuthHeader string `json:"auth_header"` // e.g., "Bearer <token>" or env var reference
}

// Config wraps TenantConfig with thread-safe access and hot-reload support.
type Config struct {
	mu       sync.RWMutex
	current  *TenantConfig
	filePath string
}

// LoadConfig loads the tenant configuration from a JSON file.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{filePath: path}
	if err := cfg.load(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Get returns the current configuration (thread-safe read).
func (c *Config) Get() *TenantConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// Reload re-reads the configuration file and swaps atomically.
func (c *Config) Reload() error {
	return c.load()
}

func (c *Config) load() error {
	data, err := os.ReadFile(c.filePath)
	if err != nil {
		return fmt.Errorf("config.load: read file: %w", err)
	}

	var tc TenantConfig
	if err := json.Unmarshal(data, &tc); err != nil {
		return fmt.Errorf("config.load: parse JSON: %w", err)
	}

	// Validate
	if tc.Tenants == nil {
		tc.Tenants = make(map[string]*StorageTarget)
	}
	for name, t := range tc.Tenants {
		if t.StorageAccount == "" {
			return fmt.Errorf("config.load: tenant %q missing storage_account", name)
		}
		if t.Container == "" {
			t.Container = "recordings" // sensible default
		}
	}
	if tc.Default != nil && tc.Default.StorageAccount == "" {
		return fmt.Errorf("config.load: default tenant missing storage_account")
	}

	c.mu.Lock()
	c.current = &tc
	c.mu.Unlock()

	return nil
}
