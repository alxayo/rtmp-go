package control

import (
	"encoding/binary"
	"testing"
)

func TestDecodeControlMessages_Golden(t *testing.T) {
	cases := []struct {
		name       string
		typeID     uint8
		goldenFile string
		assertFn   func(t *testing.T, v any)
	}{
		{
			name:       "set_chunk_size_4096",
			typeID:     TypeSetChunkSize,
			goldenFile: "control_set_chunk_size_4096.bin",
			assertFn: func(t *testing.T, v any) {
				scs, ok := v.(*SetChunkSize)
				if !ok || scs.Size != 4096 {
					t.Fatalf("unexpected SetChunkSize decode: %#v", v)
				}
			},
		},
		{
			name:       "acknowledgement_1M",
			typeID:     TypeAcknowledgement,
			goldenFile: "control_acknowledgement_1M.bin",
			assertFn: func(t *testing.T, v any) {
				ack, ok := v.(*Acknowledgement)
				if !ok || ack.SequenceNumber != 1_000_000 {
					t.Fatalf("unexpected Acknowledgement decode: %#v", v)
				}
			},
		},
		{
			name:       "window_ack_size_2_5M",
			typeID:     TypeWindowAcknowledgement,
			goldenFile: "control_window_ack_size_2_5M.bin",
			assertFn: func(t *testing.T, v any) {
				was, ok := v.(*WindowAcknowledgementSize)
				if !ok || was.Size != 2_500_000 {
					t.Fatalf("unexpected WindowAck decode: %#v", v)
				}
			},
		},
		{
			name:       "set_peer_bandwidth_dynamic",
			typeID:     TypeSetPeerBandwidth,
			goldenFile: "control_set_peer_bandwidth_dynamic.bin",
			assertFn: func(t *testing.T, v any) {
				spb, ok := v.(*SetPeerBandwidth)
				if !ok || spb.Bandwidth != 2_500_000 || spb.LimitType != 2 {
					t.Fatalf("unexpected SetPeerBandwidth decode: %#v", v)
				}
			},
		},
		{
			name:       "user_control_stream_begin",
			typeID:     TypeUserControl,
			goldenFile: "control_user_control_stream_begin.bin",
			assertFn: func(t *testing.T, v any) {
				uc, ok := v.(*UserControl)
				if !ok || uc.EventType != UCStreamBegin || uc.StreamID != 1 {
					t.Fatalf("unexpected UserControl decode: %#v", v)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := readGolden(t, tc.goldenFile)
			msg, err := Decode(tc.typeID, data)
			if err != nil {
				t.Fatalf("decode error: %v", err)
			}
			tc.assertFn(t, msg)
		})
	}
}

func TestDecodeControlMessages_AdditionalCoverage(t *testing.T) {
	// Abort Message (not part of golden vectors) round-trip style test
	var abortPayload [4]byte
	binary.BigEndian.PutUint32(abortPayload[:], 9)
	v, err := Decode(TypeAbortMessage, abortPayload[:])
	if err != nil {
		t.Fatalf("abort decode error: %v", err)
	}
	if am, ok := v.(*AbortMessage); !ok || am.CSID != 9 {
		t.Fatalf("abort decode mismatch: %#v", v)
	}

	// Ping Request & Response
	var pingReq [6]byte
	binary.BigEndian.PutUint16(pingReq[0:2], UCPingRequest)
	binary.BigEndian.PutUint32(pingReq[2:6], 123456)
	pr, err := Decode(TypeUserControl, pingReq[:])
	if err != nil {
		t.Fatalf("ping request decode error: %v", err)
	}
	if u, ok := pr.(*UserControl); !ok || u.EventType != UCPingRequest || u.Timestamp != 123456 {
		t.Fatalf("ping request decode mismatch: %#v", pr)
	}

	var pingResp [6]byte
	binary.BigEndian.PutUint16(pingResp[0:2], UCPingResponse)
	binary.BigEndian.PutUint32(pingResp[2:6], 123456)
	ps, err := Decode(TypeUserControl, pingResp[:])
	if err != nil {
		t.Fatalf("ping response decode error: %v", err)
	}
	if u, ok := ps.(*UserControl); !ok || u.EventType != UCPingResponse || u.Timestamp != 123456 {
		t.Fatalf("ping response decode mismatch: %#v", ps)
	}

	// Unknown user control event (should not error, RawData captured)
	var unknown [4]byte
	binary.BigEndian.PutUint16(unknown[0:2], 0x9999)
	copy(unknown[2:], []byte{0xAA, 0xBB})
	uv, err := Decode(TypeUserControl, unknown[:])
	if err != nil {
		t.Fatalf("unknown user control decode error: %v", err)
	}
	ucu := uv.(*UserControl)
	if ucu.EventType != 0x9999 || len(ucu.RawData) != 2 {
		t.Fatalf("unknown user control unexpected struct: %#v", ucu)
	}
}

func TestDecodeControlMessages_Errors(t *testing.T) {
	tests := []struct {
		name   string
		typeID uint8
		data   []byte
	}{
		{"set_chunk_size_len", TypeSetChunkSize, []byte{0x00, 0x00, 0x10}},                  // len!=4
		{"set_chunk_size_zero", TypeSetChunkSize, []byte{0x00, 0x00, 0x00, 0x00}},           // zero
		{"set_chunk_size_high_bit", TypeSetChunkSize, []byte{0x80, 0x00, 0x00, 0x01}},       // bit31 set
		{"ack_len", TypeAcknowledgement, []byte{0x00, 0x01}},                                // len!=4
		{"user_control_short", TypeUserControl, []byte{0x00}},                               // <2 bytes
		{"user_control_stream_begin_short", TypeUserControl, []byte{0x00, 0x00}},            // missing stream id
		{"window_ack_size_len", TypeWindowAcknowledgement, []byte{0x01, 0x02, 0x03}},        // len!=4
		{"window_ack_size_zero", TypeWindowAcknowledgement, []byte{0x00, 0x00, 0x00, 0x00}}, // zero
		{"peer_bw_len", TypeSetPeerBandwidth, []byte{0x00, 0x00, 0x00, 0x01}},               // len!=5
		{"peer_bw_limit_type", TypeSetPeerBandwidth, []byte{0x00, 0x00, 0x00, 0x01, 0x03}},  // invalid limit type
		{"unsupported_type", 99, []byte{0x00}},                                              // unsupported
		{"user_control_ping_short", TypeUserControl, []byte{0x00, 0x06, 0x01}},              // ping incomplete
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode(tt.typeID, tt.data); err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}
