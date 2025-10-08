package control

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// goldenPath resolves a golden file path relative to repo root (control package sits at internal/rtmp/control).
func goldenPath(name string) string { return filepath.Join("..", "..", "..", "tests", "golden", name) }

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	path := goldenPath(name)
	b, err := os.ReadFile(path)
	if err != nil {
		// If missing (in case repository stale), fail clearly.
		t.Fatalf("golden file %s read error: %v", name, err)
	}
	return b
}

func TestEncodeControlMessages_Golden(t *testing.T) {
	cases := []struct {
		name       string
		msg        interface{}
		goldenFile string
		typeID     uint8
		wantLen    int
	}{
		{"set_chunk_size_4096", EncodeSetChunkSize(4096), "control_set_chunk_size_4096.bin", TypeSetChunkSize, 4},
		{"acknowledgement_1M", EncodeAcknowledgement(1_000_000), "control_acknowledgement_1M.bin", TypeAcknowledgement, 4},
		{"window_ack_size_2_5M", EncodeWindowAcknowledgementSize(2_500_000), "control_window_ack_size_2_5M.bin", TypeWindowAcknowledgement, 4},
		{"set_peer_bandwidth_dynamic", EncodeSetPeerBandwidth(2_500_000, 2), "control_set_peer_bandwidth_dynamic.bin", TypeSetPeerBandwidth, 5},
		{"user_control_stream_begin", EncodeUserControlStreamBegin(1), "control_user_control_stream_begin.bin", TypeUserControl, 6},
	}

	for _, tc := range cases {
		// Each constructor returns *chunk.Message; interface{} to keep future flexibility.
		t.Run(tc.name, func(t *testing.T) {
			m := tc.msg.(*chunk.Message) // safe cast (we know all functions return *chunk.Message)
			if m.TypeID != tc.typeID {
				// import cycle avoidance: duplicate TypeID inside message test
				// (chunk.Message holds TypeID field)
				t.Fatalf("unexpected TypeID got=%d want=%d", m.TypeID, tc.typeID)
			}
			if int(m.MessageLength) != tc.wantLen || len(m.Payload) != tc.wantLen {
				t.Fatalf("payload length mismatch got msgLen=%d len(payload)=%d want=%d", m.MessageLength, len(m.Payload), tc.wantLen)
			}
			gold := readGolden(t, tc.goldenFile)
			if string(gold) != string(m.Payload) { // string compare OK for exact bytes
				// produce hex diff style display
				t.Fatalf("payload mismatch for %s\n got: % X\nwant: % X", tc.name, m.Payload, gold)
			}
			// Common control channel invariants
			if m.CSID != 2 || m.MessageStreamID != 0 || m.Timestamp != 0 {
				to := struct{ CSID, MSID, TS uint32 }{m.CSID, m.MessageStreamID, m.Timestamp}
				t.Fatalf("control channel invariants violated: %+v", to)
			}
		})
	}
}

// Additional coverage: ping request/response (no golden files yet, validate encoding semantics directly).
func TestEncodeUserControlPing(t *testing.T) {
	const ts = 123456
	pr := EncodeUserControlPingRequest(ts)
	pp := EncodeUserControlPingResponse(ts)
	if pr.TypeID != TypeUserControl || pp.TypeID != TypeUserControl {
		// ensure both are user control
		t.Fatalf("unexpected type IDs: %d %d", pr.TypeID, pp.TypeID)
	}
	if len(pr.Payload) != 6 || len(pp.Payload) != 6 {
		// 2 bytes event + 4 bytes timestamp
		t.Fatalf("unexpected payload length ping request/response: %d %d", len(pr.Payload), len(pp.Payload))
	}
	// Event markers
	if pr.Payload[0] != 0x00 || pr.Payload[1] != 0x06 { // 0x0006
		t.Fatalf("ping request event mismatch: % X", pr.Payload[:2])
	}
	if pp.Payload[0] != 0x00 || pp.Payload[1] != 0x07 { // 0x0007
		t.Fatalf("ping response event mismatch: % X", pp.Payload[:2])
	}
}

// TestAdditionalCoverage exercises remaining branches / functions for >95% coverage.
func TestAdditionalCoverage(t *testing.T) {
	abs := EncodeAbortMessage(4)
	if abs.TypeID != TypeAbortMessage || len(abs.Payload) != 4 {
		t.Fatalf("abort message encoding invalid: type=%d len=%d", abs.TypeID, len(abs.Payload))
	}
	// Cover encodeUserControl includeData=false branch (2-byte payload only)
	minimal := encodeUserControl(UCStreamBegin, 0, false)
	if len(minimal.Payload) != 2 {
		t.Fatalf("expected 2-byte minimal user control payload got=%d", len(minimal.Payload))
	}
	// Sanity: peer bandwidth already covered but call again to ensure no hidden branches
	spb := EncodeSetPeerBandwidth(123456, 1)
	if spb.Payload[4] != 1 {
		t.Fatalf("peer bandwidth limit type mismatch got=%d", spb.Payload[4])
	}
}
