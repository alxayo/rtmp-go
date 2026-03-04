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
		SubscribersActive, SubscribersTotal,
		MessagesAudio, MessagesVideo, BytesIngested,
		RelayMessagesSent, RelayMessagesDropped, RelayBytesSent,
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
		"rtmp_connections_active",
		"rtmp_connections_total",
		"rtmp_streams_active",
		"rtmp_publishers_active",
		"rtmp_publishers_total",
		"rtmp_subscribers_active",
		"rtmp_subscribers_total",
		"rtmp_messages_audio",
		"rtmp_messages_video",
		"rtmp_bytes_ingested",
		"rtmp_relay_messages_sent",
		"rtmp_relay_messages_dropped",
		"rtmp_relay_bytes_sent",
		"rtmp_uptime_seconds",
		"rtmp_server_info",
	}
	for _, key := range expectedKeys {
		if !strings.Contains(body, key) {
			t.Errorf("expvar output missing key %q", key)
		}
	}
}
