package metrics

// Expvar Metrics
// ==============
// Package-level expvar counters for live server observability.
// All variables use atomic int64 internally (via expvar.Int) and are
// safe for concurrent access from any goroutine.
//
// Gauges (values go up and down):
//   - ConnectionsActive, StreamsActive, PublishersActive, SubscribersActive
//   - RecordingsActive
//
// Counters (monotonically increasing):
//   - ConnectionsTotal, PublishersTotal, SubscribersTotal
//   - MessagesAudio, MessagesVideo, BytesIngested, BytesEgress
//   - SubscriberDropsTotal, AuthSuccessesTotal, AuthFailuresTotal
//   - HandshakeFailuresTotal, RecordingErrorsTotal, ZombieConnectionsTotal
//   - RelayMessagesSent, RelayMessagesDropped, RelayBytesSent
//
// Dynamic endpoints (expvar.Func, computed per HTTP request):
//   - rtmp_streams: per-stream JSON (key, subscribers, codecs, uptime)
//   - rtmp_relay_destinations: per-destination JSON (url, status, metrics)

import (
	"expvar"
	"runtime"
	"sync"
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
	SubscribersActive    = expvar.NewInt("rtmp_subscribers_active")
	SubscribersTotal     = expvar.NewInt("rtmp_subscribers_total")
	SubscriberDropsTotal = expvar.NewInt("rtmp_subscriber_drops_total")
)

// ── Auth metrics ────────────────────────────────────────────────────

var (
	AuthSuccessesTotal = expvar.NewInt("rtmp_auth_successes_total")
	AuthFailuresTotal  = expvar.NewInt("rtmp_auth_failures_total")
)

// ── Media metrics ───────────────────────────────────────────────────

var (
	MessagesAudio = expvar.NewInt("rtmp_messages_audio")
	MessagesVideo = expvar.NewInt("rtmp_messages_video")
	BytesIngested = expvar.NewInt("rtmp_bytes_ingested")
	BytesEgress   = expvar.NewInt("rtmp_bytes_egress")
)

// ── Handshake metrics ───────────────────────────────────────────────

var (
	HandshakeFailuresTotal = expvar.NewInt("rtmp_handshake_failures_total")
)

// ── Recording metrics ───────────────────────────────────────────────

var (
	RecordingsActive     = expvar.NewInt("rtmp_recordings_active")
	RecordingErrorsTotal = expvar.NewInt("rtmp_recording_errors_total")
)

// ── Connection health metrics ───────────────────────────────────────

var (
	ZombieConnectionsTotal = expvar.NewInt("rtmp_zombie_connections_total")
)

// ── Relay metrics ───────────────────────────────────────────────────

var (
	RelayMessagesSent    = expvar.NewInt("rtmp_relay_messages_sent")
	RelayMessagesDropped = expvar.NewInt("rtmp_relay_messages_dropped")
	RelayBytesSent       = expvar.NewInt("rtmp_relay_bytes_sent")
)

// ── SRT metrics ─────────────────────────────────────────────────────

var (
	// SRTConnectionsActive tracks currently connected SRT publishers (gauge).
	SRTConnectionsActive = expvar.NewInt("srt_connections_active")

	// SRTConnectionsTotal counts all SRT connections ever accepted (counter).
	SRTConnectionsTotal = expvar.NewInt("srt_connections_total")

	// SRTBytesReceived counts total bytes received over SRT (counter).
	SRTBytesReceived = expvar.NewInt("srt_bytes_received")

	// SRTPacketsReceived counts total data packets received over SRT (counter).
	SRTPacketsReceived = expvar.NewInt("srt_packets_received")

	// SRTPacketsRetransmit counts retransmitted packets over SRT (counter).
	SRTPacketsRetransmit = expvar.NewInt("srt_packets_retransmit")

	// SRTPacketsDropped counts packets dropped due to too-late delivery (counter).
	SRTPacketsDropped = expvar.NewInt("srt_packets_dropped")
)

// ── Dynamic snapshot endpoints ──────────────────────────────────────

// snapshotMu protects the snapshot function registrations.
var snapshotMu sync.RWMutex

// streamSnapshotFn and relaySnapshotFn hold the registered providers.
// The expvar.Func wrappers (registered once in init) delegate to these.
var (
	streamSnapshotFn func() interface{}
	relaySnapshotFn  func() interface{}
)

// RegisterStreamSnapshot sets the function that returns per-stream info
// as a JSON-serializable value. Call from server startup after the
// registry is created. Safe to call multiple times (e.g., in tests).
func RegisterStreamSnapshot(fn func() interface{}) {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	streamSnapshotFn = fn
}

// RegisterRelaySnapshot sets the function that returns per-destination
// relay info as a JSON-serializable value. Call from server startup after
// the destination manager is created. Safe to call multiple times.
func RegisterRelaySnapshot(fn func() interface{}) {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	relaySnapshotFn = fn
}

func init() {
	expvar.Publish("rtmp_uptime_seconds", expvar.Func(func() interface{} {
		return int64(time.Since(startTime).Seconds())
	}))

	expvar.Publish("rtmp_server_info", expvar.Func(func() interface{} {
		return map[string]string{
			"go_version": runtime.Version(),
		}
	}))

	// Per-stream and per-destination endpoints are registered once here.
	// The actual provider functions are set later via RegisterStreamSnapshot
	// and RegisterRelaySnapshot. Returns empty arrays until a provider is set.
	expvar.Publish("rtmp_streams", expvar.Func(func() interface{} {
		snapshotMu.RLock()
		fn := streamSnapshotFn
		snapshotMu.RUnlock()
		if fn == nil {
			return []interface{}{}
		}
		return fn()
	}))

	expvar.Publish("rtmp_relay_destinations", expvar.Func(func() interface{} {
		snapshotMu.RLock()
		fn := relaySnapshotFn
		snapshotMu.RUnlock()
		if fn == nil {
			return []interface{}{}
		}
		return fn()
	}))
}
