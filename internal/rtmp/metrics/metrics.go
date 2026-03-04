package metrics

// Expvar Metrics
// ==============
// Package-level expvar counters for live server observability.
// All variables use atomic int64 internally (via expvar.Int) and are
// safe for concurrent access from any goroutine.
//
// Gauges (values go up and down):
//   - ConnectionsActive, StreamsActive, PublishersActive, SubscribersActive
//
// Counters (monotonically increasing):
//   - ConnectionsTotal, PublishersTotal, SubscribersTotal
//   - MessagesAudio, MessagesVideo, BytesIngested
//   - RelayMessagesSent, RelayMessagesDropped, RelayBytesSent

import (
	"expvar"
	"runtime"
	"time"
)

// startTime records when the package was initialized (≈ process start).
var startTime = time.Now()

// ── Connection metrics ──────────────────────────────────────────────

var (
	ConnectionsActive = expvar.NewInt("rtmp_connections_active")
	ConnectionsTotal  = expvar.NewInt("rtmp_connections_total")
)

// ── Stream metrics ──────────────────────────────────────────────────

var (
	StreamsActive = expvar.NewInt("rtmp_streams_active")
)

// ── Publisher metrics ───────────────────────────────────────────────

var (
	PublishersActive = expvar.NewInt("rtmp_publishers_active")
	PublishersTotal  = expvar.NewInt("rtmp_publishers_total")
)

// ── Subscriber metrics ──────────────────────────────────────────────

var (
	SubscribersActive = expvar.NewInt("rtmp_subscribers_active")
	SubscribersTotal  = expvar.NewInt("rtmp_subscribers_total")
)

// ── Media metrics ───────────────────────────────────────────────────

var (
	MessagesAudio = expvar.NewInt("rtmp_messages_audio")
	MessagesVideo = expvar.NewInt("rtmp_messages_video")
	BytesIngested = expvar.NewInt("rtmp_bytes_ingested")
)

// ── Relay metrics ───────────────────────────────────────────────────

var (
	RelayMessagesSent    = expvar.NewInt("rtmp_relay_messages_sent")
	RelayMessagesDropped = expvar.NewInt("rtmp_relay_messages_dropped")
	RelayBytesSent       = expvar.NewInt("rtmp_relay_bytes_sent")
)

func init() {
	expvar.Publish("rtmp_uptime_seconds", expvar.Func(func() interface{} {
		return int64(time.Since(startTime).Seconds())
	}))

	expvar.Publish("rtmp_server_info", expvar.Func(func() interface{} {
		return map[string]string{
			"go_version": runtime.Version(),
		}
	}))
}
