// Package logger provides structured JSON logging with dynamic log level control.
//
// The logger is built on top of Go's standard log/slog package and outputs JSON
// to stdout, making it compatible with log aggregation systems (Elasticsearch,
// CloudWatch, etc.).
//
// # Initialization
//
// Call logger.Init() once at application startup. It is safe to call multiple
// times (subsequent calls are no-ops). The initial log level is determined from
// (in order of precedence):
//   - Command-line flag: -log-level=debug or -log.level=debug
//   - Environment variable: RTMP_LOG_LEVEL=debug
//   - Default: info
//
// Supported levels: debug, info, warn, error
//
//	logger.Init()
//	log := logger.Default()
//	log.Info("server started", "addr", "localhost:1935")
//
// # Dynamic Level Changes
//
// The log level can be changed at runtime without restarting:
//
//	logger.SetLevel(slog.LevelDebug)  // Switch to debug logging
//	logger.SetLevel(slog.LevelInfo)   // Switch back to info
//
// This is useful for temporary debugging or responding to admin commands.
//
// # Integration with Global slog
//
// The logger is installed as the global slog.Logger via slog.SetDefault(),
// so any code using slog.Default() gets the configured logger with the
// correct level. This is especially important for third-party libraries
// like the SRT listener that use slog.
//
// # Output Format
//
// All messages are JSON objects with these standard fields:
//   - time: RFC3339 timestamp with nanoseconds
//   - level: log level (debug, info, warn, error)
//   - msg: message text
//   - Additional fields passed as key-value pairs: log.Info("msg", "key", value)
//
// Example output:
//
//	{"time":"2026-04-11T10:33:56.011379+03:00","level":"INFO","msg":"server started","component":"cli"}
//
// # Usage in RTMP/SRT Code
//
// The main server code receives a logger instance via dependency injection:
//
//	type Server struct {
//	    logger *slog.Logger
//	}
//
//	func (s *Server) handlePublish() {
//	    s.logger.Info("publish started",
//	        "stream_key", streamKey,
//	        "conn_id", connID,
//	        "remote_addr", remoteAddr,
//	    )
//	}
//
// Always include context fields (stream_key, conn_id, type_id, timestamp) to
// aid in debugging and tracing requests through the system.
package logger
