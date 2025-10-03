package logger

import (
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Environment variable name for log level configuration.
const envLogLevel = "RTMP_LOG_LEVEL"

var (
	// atomicLevel implements slog.Leveler and can be changed at runtime.
	atomicLevel = &dynamicLevel{v: int64(slog.LevelInfo)}
	// global logger instance
	global     *slog.Logger
	initOnce   sync.Once
	writerOnce sync.Once

	// Optional flag (users may pass -log.level=debug). If flags.Parse() hasn't
	// yet been called when Init is invoked, we still read the raw os.Args.
	flagLevel = flag.String("log.level", "", "log level (debug, info, warn, error)")
)

// dynamicLevel is an atomic Leveler.
type dynamicLevel struct{ v int64 }

func (d *dynamicLevel) Level() slog.Level { return slog.Level(atomic.LoadInt64(&d.v)) }
func (d *dynamicLevel) set(l slog.Level)  { atomic.StoreInt64(&d.v, int64(l)) }

// Init initializes the global logger. It is safe to call multiple times; the
// first call wins except SetLevel / UseWriter which mutate state intentionally.
func Init() {
	initOnce.Do(func() {
		lvl := detectLevel()
		atomicLevel.set(lvl)
		global = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: atomicLevel}))
	})
}

// detectLevel resolves the initial log level from (precedence high→low):
//  1. command-line flag -log.level
//  2. environment variable RTMP_LOG_LEVEL
//  3. default (info)
func detectLevel() slog.Level {
	// Attempt to parse flag value (handles both parsed & unparsed states).
	if *flagLevel == "" {
		for _, arg := range os.Args[1:] {
			if strings.HasPrefix(arg, "-log.level=") {
				parts := strings.SplitN(arg, "=", 2)
				if len(parts) == 2 {
					*flagLevel = parts[1]
				}
			}
		}
	}
	if lvl, ok := parseLevel(strings.TrimSpace(*flagLevel)); ok {
		return lvl
	}
	if env := os.Getenv(envLogLevel); env != "" {
		if lvl, ok := parseLevel(env); ok {
			return lvl
		}
	}
	return slog.LevelInfo
}

// parseLevel converts string to slog.Level.
func parseLevel(s string) (slog.Level, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "debug":
		return slog.LevelDebug, true
	case "info", "":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error", "err":
		return slog.LevelError, true
	}
	return 0, false
}

// SetLevel changes the runtime log level.
func SetLevel(level string) error {
	Init()
	lvl, ok := parseLevel(level)
	if !ok {
		return errors.New("invalid log level: " + level)
	}
	atomicLevel.set(lvl)
	return nil
}

// Level returns the current runtime level as string.
func Level() string {
	Init()
	return atomicLevel.Level().String()
}

// UseWriter swaps the output writer (intended for tests). Retains current level.
func UseWriter(w io.Writer) {
	Init()
	global = slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: atomicLevel}))
}

// Logger returns the global logger (ensures Init was called).
func Logger() *slog.Logger { Init(); return global }

// Convenience top-level logging functions.
func Debug(msg string, args ...any) { Logger().Debug(msg, args...) }
func Info(msg string, args ...any)  { Logger().Info(msg, args...) }
func Warn(msg string, args ...any)  { Logger().Warn(msg, args...) }
func Error(msg string, args ...any) { Logger().Error(msg, args...) }

// WithConn attaches connection identity fields.
func WithConn(l *slog.Logger, connID, peerAddr string) *slog.Logger {
	return l.With("conn_id", connID, "peer_addr", peerAddr)
}

// WithStream attaches the stream key.
func WithStream(l *slog.Logger, streamKey string) *slog.Logger {
	return l.With("stream_key", streamKey)
}

// WithMessageMeta attaches message metadata fields. Timestamp is an RTMP timestamp
// in milliseconds if provided (>0). If ts==0 it uses current time in ms.
func WithMessageMeta(l *slog.Logger, msgType string, csid int, msid uint32, ts uint32) *slog.Logger {
	if ts == 0 {
		// Provide RTMP-style millisecond timestamp relative to start (approx current Unix ms)
		ms := uint32(time.Now().UnixMilli() & 0xFFFFFFFF)
		return l.With("msg_type", msgType, "csid", csid, "msid", msid, "timestamp", ms)
	}
	return l.With("msg_type", msgType, "csid", csid, "msid", msid, "timestamp", ts)
}
