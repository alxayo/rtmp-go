package main

// SegmentNotifier polls HLS output directories for new segment files and
// playlist updates, then sends segment_complete webhook events to the
// blob-sidecar for upload to Azure Blob Storage.
//
// Design decisions:
//   - Poll-based (not fsnotify) because Azure Files SMB mounts don't
//     reliably support inotify/kqueue
//   - Tracks .ts segments by name (immutable once written, uploaded once)
//   - Tracks .m3u8 playlists by mod time (rewritten as new segments arrive)
//   - Uses "hls/{safeKey}" stream key prefix for tenant routing in blob-sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SegmentNotifier watches an HLS output directory and fires webhook events
// to blob-sidecar for each new segment or updated playlist.
type SegmentNotifier struct {
	webhookURL   string
	logger       *slog.Logger
	pollInterval time.Duration
	client       *http.Client
}

// NewSegmentNotifier creates a notifier that sends events to the given webhook URL.
// If webhookURL is empty, the notifier is a no-op.
func NewSegmentNotifier(webhookURL string, logger *slog.Logger) *SegmentNotifier {
	return &SegmentNotifier{
		webhookURL:   webhookURL,
		logger:       logger,
		pollInterval: 3 * time.Second, // Slightly longer than HLS segment duration (2s)
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Enabled returns true if the notifier has a webhook URL configured.
func (n *SegmentNotifier) Enabled() bool {
	return n.webhookURL != ""
}

// WatchStream polls the output directory for new HLS files and fires webhook
// events to blob-sidecar. Blocks until ctx is cancelled.
func (n *SegmentNotifier) WatchStream(ctx context.Context, streamKey, outputDir string) {
	if !n.Enabled() {
		return
	}

	seen := make(map[string]struct{})         // .ts segments (immutable once written)
	playlistMods := make(map[string]time.Time) // .m3u8 last-seen mod times

	safeKey := sanitizeStreamKey(streamKey)

	ticker := time.NewTicker(n.pollInterval)
	defer ticker.Stop()

	n.logger.Info("segment notifier started", "stream_key", streamKey, "output_dir", outputDir)

	for {
		select {
		case <-ctx.Done():
			n.logger.Info("segment notifier stopped", "stream_key", streamKey)
			return
		case <-ticker.C:
			// Self-heal: re-create master.m3u8 if it's been deleted.
			// Azure Files SMB can lose files due to rename/caching quirks.
			n.ensureMasterPlaylist(outputDir)
			n.scanDir(ctx, safeKey, outputDir, seen, playlistMods)
		}
	}
}

// ensureMasterPlaylist re-creates master.m3u8 if it's missing from the output
// directory. This provides self-healing on Azure Files SMB where the file can
// be lost due to caching or rename anomalies.
func (n *SegmentNotifier) ensureMasterPlaylist(outputDir string) {
	masterPath := filepath.Join(outputDir, "master.m3u8")
	if _, err := os.Stat(masterPath); err == nil {
		return // file exists, nothing to do
	}
	if err := writeMasterPlaylist(outputDir); err != nil {
		n.logger.Warn("failed to re-create master.m3u8", "dir", outputDir, "error", err)
	} else {
		n.logger.Warn("re-created missing master.m3u8", "dir", outputDir)
	}
}

// scanDir walks the output directory tree and fires events for new files.
func (n *SegmentNotifier) scanDir(ctx context.Context, safeKey, dir string, seen map[string]struct{}, playlistMods map[string]time.Time) {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".ts":
			// Segments are immutable — upload once when first seen
			if _, ok := seen[path]; !ok {
				seen[path] = struct{}{}
				n.notify(ctx, safeKey, dir, path)
			}
		case ".m3u8":
			// Playlists are rewritten as new segments arrive — re-upload on change
			lastMod, ok := playlistMods[path]
			if !ok || info.ModTime().After(lastMod) {
				playlistMods[path] = info.ModTime()
				n.notify(ctx, safeKey, dir, path)
			}
		}

		return nil
	})
	if err != nil {
		n.logger.Warn("scan error", "dir", dir, "error", err)
	}
}

// notify sends a segment_complete webhook event to blob-sidecar.
//
// The stream key is constructed to preserve the HLS directory structure in blob
// storage. The blob-sidecar builds blob paths as {path_prefix}/{stream_key}/{filename},
// so we encode the subdirectory into the stream key:
//
//   - master.m3u8        → stream_key: "hls/live_stream1"
//   - stream_0/seg_0.ts  → stream_key: "hls/live_stream1/stream_0"
//
// The "hls/" prefix matches the "hls" tenant in the blob-sidecar config,
// routing uploads to the hls-content blob container.
func (n *SegmentNotifier) notify(ctx context.Context, safeKey, outputDir, filePath string) {
	// Compute relative path to preserve directory structure
	rel, err := filepath.Rel(outputDir, filePath)
	if err != nil {
		rel = filepath.Base(filePath)
	}

	// Build stream key: "hls/{safeKey}" for root files, "hls/{safeKey}/{subdir}" for renditions
	streamKey := "hls/" + safeKey
	relDir := filepath.Dir(rel)
	if relDir != "." {
		streamKey = streamKey + "/" + filepath.ToSlash(relDir)
	}

	event := map[string]interface{}{
		"type":       "segment_complete",
		"timestamp":  time.Now().Unix(),
		"conn_id":    "hls-transcoder",
		"stream_key": streamKey,
		"data": map[string]interface{}{
			"path": filePath,
		},
	}

	body, err := json.Marshal(event)
	if err != nil {
		n.logger.Error("failed to marshal segment event", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		n.logger.Error("failed to create segment event request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return // context cancelled during shutdown — expected
		}
		n.logger.Warn("failed to send segment event", "path", filePath, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		n.logger.Warn("segment event rejected", "path", filePath, "status", resp.StatusCode)
		return
	}

	n.logger.Debug("segment event sent", "stream_key", streamKey, "file", filepath.Base(filePath))
}

// buildEventStreamKey constructs the stream key used in webhook events.
// Exported for testing.
func buildEventStreamKey(safeKey, outputDir, filePath string) string {
	rel, err := filepath.Rel(outputDir, filePath)
	if err != nil {
		rel = filepath.Base(filePath)
	}

	streamKey := "hls/" + safeKey
	relDir := filepath.Dir(rel)
	if relDir != "." {
		streamKey = streamKey + "/" + filepath.ToSlash(relDir)
	}
	return streamKey
}
