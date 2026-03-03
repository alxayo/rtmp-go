package auth

import (
	"net/url"
	"strings"
)

// ParsedStreamURL holds a stream name separated from its query parameters.
// RTMP clients (OBS, FFmpeg) embed tokens in the stream name field like
// "mystream?token=abc123". This struct splits that into the clean name and
// the parsed key-value pairs.
type ParsedStreamURL struct {
	StreamName  string            // clean name without query string (e.g. "mystream")
	QueryParams map[string]string // parsed parameters (e.g. {"token": "abc123"})
}

// ParseStreamURL splits a raw stream name (as received in publish/play
// commands) into the clean stream name and any query parameters appended
// after "?".
//
// Examples:
//
//	"mystream"                     → {StreamName: "mystream",  QueryParams: {}}
//	"mystream?token=abc123"        → {StreamName: "mystream",  QueryParams: {"token": "abc123"}}
//	"mystream?token=a&expires=123" → {StreamName: "mystream",  QueryParams: {"token": "a", "expires": "123"}}
//	""                             → {StreamName: "",           QueryParams: {}}
func ParseStreamURL(raw string) *ParsedStreamURL {
	result := &ParsedStreamURL{QueryParams: make(map[string]string)}

	// Find the query separator
	idx := strings.IndexByte(raw, '?')
	if idx < 0 {
		// No query parameters — the entire string is the stream name
		result.StreamName = raw
		return result
	}

	// Split at the "?" boundary
	result.StreamName = raw[:idx]
	if idx+1 < len(raw) {
		values, err := url.ParseQuery(raw[idx+1:])
		if err == nil {
			for k, v := range values {
				if len(v) > 0 {
					result.QueryParams[k] = v[0]
				}
			}
		}
	}

	return result
}
