// Package integration – end-to-end tests for the RTMP server.
//
// metrics_test.go validates the expvar metrics HTTP endpoint.
// It starts an HTTP listener (mirroring what main.go does with -metrics-addr),
// then queries /debug/vars and verifies all rtmp_* keys are present with
// correct initial values.
package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	_ "github.com/alxayo/go-rtmp/internal/rtmp/metrics" // Register expvar counters
	srv "github.com/alxayo/go-rtmp/internal/rtmp/server"
)

// TestMetricsEndpoint verifies the expvar HTTP endpoint serves all RTMP counters.
func TestMetricsEndpoint(t *testing.T) {
	// Start RTMP server on ephemeral port
	s := srv.New(srv.Config{
		ListenAddr: "127.0.0.1:0",
		ChunkSize:  4096,
	})
	if err := s.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer s.Stop()

	// Start HTTP metrics server on ephemeral port (mirrors main.go behavior)
	metricsLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("metrics listen: %v", err)
	}
	defer metricsLn.Close()
	metricsAddr := metricsLn.Addr().String()

	httpSrv := &http.Server{Handler: http.DefaultServeMux}
	go func() { _ = httpSrv.Serve(metricsLn) }()
	defer httpSrv.Close()

	// Give the HTTP server a moment to start
	time.Sleep(50 * time.Millisecond)

	// Query /debug/vars
	resp, err := http.Get(fmt.Sprintf("http://%s/debug/vars", metricsAddr))
	if err != nil {
		t.Fatalf("GET /debug/vars: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var vars map[string]json.RawMessage
	if err := json.Unmarshal(body, &vars); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}

	// Verify all RTMP counter keys exist
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
		if _, ok := vars[key]; !ok {
			t.Errorf("missing key %q in /debug/vars output", key)
		}
	}

	// Verify uptime is >= 0
	var uptime int64
	if err := json.Unmarshal(vars["rtmp_uptime_seconds"], &uptime); err != nil {
		t.Fatalf("parse uptime: %v", err)
	}
	if uptime < 0 {
		t.Errorf("uptime should be >= 0, got %d", uptime)
	}

	// Verify server_info contains go_version
	var serverInfo map[string]string
	if err := json.Unmarshal(vars["rtmp_server_info"], &serverInfo); err != nil {
		t.Fatalf("parse server_info: %v", err)
	}
	if _, ok := serverInfo["go_version"]; !ok {
		t.Error("server_info missing go_version key")
	}
}
