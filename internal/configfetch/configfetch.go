// Package configfetch fetches missing configuration values from the platform
// API at startup. It checks which env vars are absent, requests only those
// keys, and sets them in the process environment so downstream code can use
// os.Getenv as usual.
package configfetch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// ConfigResponse matches the platform API shape: { data: { key: value, ... } }
type ConfigResponse struct {
	Data map[string]string `json:"data"`
}

// FetchRemoteConfig fetches the given keys from the platform config API,
// but only those not already present in the environment. Fetched values are
// injected into os environment so later os.Getenv calls pick them up.
//
// Returns the map of keys that were actually fetched and set, or an error
// if the request itself failed. Both platformURL and apiKey must be non-empty
// for any fetch to happen; if either is missing the call is a silent no-op.
func FetchRemoteConfig(platformURL, apiKey string, keys []string) (map[string]string, error) {
	if platformURL == "" || apiKey == "" {
		return nil, nil
	}

	var needed []string
	for _, key := range keys {
		if os.Getenv(key) == "" {
			needed = append(needed, key)
		}
	}
	if len(needed) == 0 {
		return nil, nil
	}

	url := fmt.Sprintf("%s/api/internal/config?keys=%s",
		strings.TrimRight(platformURL, "/"),
		strings.Join(needed, ","),
	)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("config fetch: create request: %w", err)
	}
	req.Header.Set("X-Internal-Api-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("config fetch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config fetch: unexpected status %d", resp.StatusCode)
	}

	var result ConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("config fetch: decode response: %w", err)
	}

	fetched := make(map[string]string)
	for key, value := range result.Data {
		if value != "" {
			os.Setenv(key, value)
			fetched[key] = value
		}
	}

	return fetched, nil
}
