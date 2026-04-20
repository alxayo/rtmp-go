package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a directory tree for completed segment files.
// It uses fsnotify for filesystem events and a stabilization timer to ensure
// files are fully written before triggering the upload callback.
type Watcher struct {
	dir          string
	stabilize    time.Duration
	logger       *slog.Logger
	onComplete   func(path string) // called when a segment file is ready
	fsWatcher    *fsnotify.Watcher
	pending      map[string]*time.Timer // files waiting for stabilization
	mu           sync.Mutex
	stopOnce     sync.Once
	done         chan struct{}
}

// NewWatcher creates a filesystem watcher that monitors dir recursively.
// onComplete is called (in a goroutine) when a segment file is considered
// fully written (no modifications for stabilizeDur).
func NewWatcher(dir string, stabilizeDur time.Duration, logger *slog.Logger, onComplete func(string)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		dir:        dir,
		stabilize:  stabilizeDur,
		logger:     logger,
		onComplete: onComplete,
		fsWatcher:  fsw,
		pending:    make(map[string]*time.Timer),
		done:       make(chan struct{}),
	}, nil
}

// Start begins watching the directory. It adds all existing subdirectories
// and processes events until ctx is cancelled.
func (w *Watcher) Start(ctx context.Context) error {
	// Ensure watch directory exists
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}

	// Add watch directory and all subdirectories
	if err := w.addRecursive(w.dir); err != nil {
		return err
	}

	// Also scan for any existing files that might have been missed
	go w.scanExisting()

	// Event loop
	go w.eventLoop(ctx)

	w.logger.Info("watcher started", "dir", w.dir, "stabilize", w.stabilize)
	return nil
}

// Stop halts the watcher and cleans up.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		w.fsWatcher.Close()
		close(w.done)

		// Cancel all pending timers
		w.mu.Lock()
		for _, timer := range w.pending {
			timer.Stop()
		}
		w.pending = nil
		w.mu.Unlock()
	})
}

func (w *Watcher) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.done:
			return
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("fsnotify error", "error", err)
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	// If a new directory is created, watch it recursively
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			w.addRecursive(path)
			return
		}
	}

	// Only process media segment files
	if !isSegmentFile(path) {
		return
	}

	// On create or write events, reset the stabilization timer.
	// The file is considered "complete" only after no writes for stabilize duration.
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		w.resetStabilizationTimer(path)
	}
}

func (w *Watcher) resetStabilizationTimer(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.pending == nil {
		return // shutting down
	}

	// Cancel existing timer for this file if any
	if timer, exists := w.pending[path]; exists {
		timer.Stop()
	}

	// Set new stabilization timer
	w.pending[path] = time.AfterFunc(w.stabilize, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()

		// Verify file still exists and has content
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			return
		}

		w.logger.Debug("segment stabilized", "path", path, "size", info.Size())
		go w.onComplete(path)
	})
}

// scanExisting processes any segment files already present in the directory.
// This handles the case where the sidecar restarts and segments were missed.
func (w *Watcher) scanExisting() {
	filepath.Walk(w.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !isSegmentFile(path) {
			return nil
		}
		// For existing files, check if they're old enough (haven't been written to recently)
		if time.Since(info.ModTime()) > w.stabilize {
			w.logger.Info("found existing segment", "path", path, "size", info.Size())
			go w.onComplete(path)
		}
		return nil
	})
}

func (w *Watcher) addRecursive(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible directories
		}
		if info.IsDir() {
			if err := w.fsWatcher.Add(path); err != nil {
				w.logger.Warn("failed to watch directory", "path", path, "error", err)
			}
		}
		return nil
	})
}

// isSegmentFile returns true if the file is a media segment (FLV or MP4).
func isSegmentFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".flv" || ext == ".mp4"
}
