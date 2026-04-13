package metrics

import (
	"encoding/json"
	"expvar"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestCountersInitializedToZero(t *testing.T) {
	counters := []*expvar.Int{
		ConnectionsActive, ConnectionsTotal,
		StreamsActive,
		PublishersActive, PublishersTotal,
		SubscribersActive, SubscribersTotal, SubscriberDropsTotal,
		AuthSuccessesTotal, AuthFailuresTotal,
		MessagesAudio, MessagesVideo, BytesIngested, BytesEgress,
		HandshakeFailuresTotal,
		RecordingsActive, RecordingErrorsTotal,
		ZombieConnectionsTotal,
		RelayMessagesSent, RelayMessagesDropped, RelayBytesSent,
		SRTConnectionsActive, SRTConnectionsTotal,
		SRTBytesReceived, SRTPacketsReceived, SRTPacketsRetransmit, SRTPacketsDropped,
	}
	for _, c := range counters {
		if v := c.Value(); v != 0 {
			t.Errorf("counter should be 0, got %d", v)
		}
	}
}

func TestGaugeAddAndSubtract(t *testing.T) {
	// Use ConnectionsActive as a representative gauge.
	defer ConnectionsActive.Set(0)

	ConnectionsActive.Add(1)
	ConnectionsActive.Add(1)
	ConnectionsActive.Add(1)
	if v := ConnectionsActive.Value(); v != 3 {
		t.Fatalf("expected 3, got %d", v)
	}

	ConnectionsActive.Add(-1)
	if v := ConnectionsActive.Value(); v != 2 {
		t.Fatalf("expected 2, got %d", v)
	}
}

func TestCounterMonotonic(t *testing.T) {
	defer ConnectionsTotal.Set(0)

	for i := 0; i < 100; i++ {
		ConnectionsTotal.Add(1)
	}
	if v := ConnectionsTotal.Value(); v != 100 {
		t.Fatalf("expected 100, got %d", v)
	}
}

func TestUptimePositive(t *testing.T) {
	v := expvar.Get("rtmp_uptime_seconds")
	if v == nil {
		t.Fatal("rtmp_uptime_seconds not registered")
	}
	// The func returns int64; expvar serializes it as a number.
	raw := v.String()
	if raw == "" {
		t.Fatal("empty uptime string")
	}
	// Uptime should be ≥ 0 (could be 0 if test runs within the first second)
	var uptime int64
	if err := json.Unmarshal([]byte(raw), &uptime); err != nil {
		t.Fatalf("failed to parse uptime: %v", err)
	}
	if uptime < 0 {
		t.Fatalf("uptime should be >= 0, got %d", uptime)
	}
}

func TestServerInfoContainsGoVersion(t *testing.T) {
	v := expvar.Get("rtmp_server_info")
	if v == nil {
		t.Fatal("rtmp_server_info not registered")
	}
	raw := v.String()
	var info map[string]string
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatalf("failed to parse server_info: %v", err)
	}
	goVer, ok := info["go_version"]
	if !ok {
		t.Fatal("go_version key missing from server_info")
	}
	if goVer != runtime.Version() {
		t.Fatalf("expected %s, got %s", runtime.Version(), goVer)
	}
}

func TestExpvarHandlerContainsRTMPKeys(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/vars", nil)
	expvar.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	expectedKeys := []string{
		// Connection metrics
		"rtmp_connections_active",
		"rtmp_connections_total",
		// Stream metrics
		"rtmp_streams_active",
		// Publisher metrics
		"rtmp_publishers_active",
		"rtmp_publishers_total",
		// Subscriber metrics
		"rtmp_subscribers_active",
		"rtmp_subscribers_total",
		"rtmp_subscriber_drops_total",
		// Auth metrics
		"rtmp_auth_successes_total",
		"rtmp_auth_failures_total",
		// Media metrics
		"rtmp_messages_audio",
		"rtmp_messages_video",
		"rtmp_bytes_ingested",
		"rtmp_bytes_egress",
		// Handshake metrics
		"rtmp_handshake_failures_total",
		// Recording metrics
		"rtmp_recordings_active",
		"rtmp_recording_errors_total",
		// Connection health
		"rtmp_zombie_connections_total",
		// Relay metrics
		"rtmp_relay_messages_sent",
		"rtmp_relay_messages_dropped",
		"rtmp_relay_bytes_sent",
		// SRT metrics
		"srt_connections_active",
		"srt_connections_total",
		"srt_bytes_received",
		"srt_packets_received",
		"srt_packets_retransmit",
		"srt_packets_dropped",
		// Info
		"rtmp_uptime_seconds",
		"rtmp_server_info",
	}
	for _, key := range expectedKeys {
		if !strings.Contains(body, key) {
			t.Errorf("expvar output missing key %q", key)
		}
	}
}

func TestRegisterStreamSnapshot(t *testing.T) {
	testData := []map[string]interface{}{
		{"key": "live/test", "subscribers": 2},
	}
	RegisterStreamSnapshot(func() interface{} {
		return testData
	})

	v := expvar.Get("rtmp_streams")
	if v == nil {
		t.Fatal("rtmp_streams not registered")
	}

	raw := v.String()
	if !strings.Contains(raw, "live/test") {
		t.Errorf("rtmp_streams should contain stream key, got %s", raw)
	}
}

func TestRegisterRelaySnapshot(t *testing.T) {
	testData := []map[string]interface{}{
		{"url": "rtmp://cdn.example.com/live/key", "status": "connected"},
	}
	RegisterRelaySnapshot(func() interface{} {
		return testData
	})

	v := expvar.Get("rtmp_relay_destinations")
	if v == nil {
		t.Fatal("rtmp_relay_destinations not registered")
	}

	raw := v.String()
	if !strings.Contains(raw, "cdn.example.com") {
		t.Errorf("rtmp_relay_destinations should contain destination URL, got %s", raw)
	}
}
