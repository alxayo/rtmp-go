package ts

import (
	"testing"
)

// encodePTS encodes a 33-bit PTS value into the 5-byte format used in PES headers.
// This helper builds the exact binary encoding so we can verify our parser.
//
// The 5-byte format interleaves timestamp bits with marker bits:
//
//	Byte 0: prefix(4) | ts[32:30](3) | marker(1)
//	Byte 1: ts[29:22](8)
//	Byte 2: ts[21:15](7) | marker(1)
//	Byte 3: ts[14:7](8)
//	Byte 4: ts[6:0](7) | marker(1)
func encodePTS(prefix byte, ts int64) []byte {
	buf := make([]byte, 5)
	// Byte 0: prefix in high 4 bits, then bits 32-30, then marker bit=1
	buf[0] = (prefix << 4) | (byte((ts>>29)&0x0E) | 0x01)
	// Bytes 1-2: bits 29-15 with marker bit at end
	buf[1] = byte(ts >> 22)
	buf[2] = byte((ts>>14)&0xFE) | 0x01
	// Bytes 3-4: bits 14-0 with marker bit at end
	buf[3] = byte(ts >> 7)
	buf[4] = byte((ts<<1)&0xFE) | 0x01
	return buf
}

// buildPESPacket constructs a PES packet with optional PTS and DTS.
func buildPESPacket(streamID byte, pts, dts int64, payload []byte) []byte {
	// Start with PES start code prefix (0x000001).
	pes := []byte{0x00, 0x00, 0x01, streamID}

	// Determine PTS/DTS flags and header data.
	var ptsDtsFlags byte
	var headerData []byte

	switch {
	case pts >= 0 && dts >= 0 && pts != dts:
		// Both PTS and DTS present (different values, like B-frames).
		ptsDtsFlags = 0x03
		headerData = append(headerData, encodePTS(0x03, pts)...)
		headerData = append(headerData, encodePTS(0x01, dts)...)
	case pts >= 0:
		// PTS only (common case for audio and non-B-frame video).
		ptsDtsFlags = 0x02
		headerData = append(headerData, encodePTS(0x02, pts)...)
	default:
		// No timestamps.
		ptsDtsFlags = 0x00
	}

	// PES packet length: 3 (fixed optional header bytes) + headerData + payload.
	// For video, PES length can be 0 (unbounded), but for testing we set it properly.
	pesLength := 3 + len(headerData) + len(payload)

	// PES packet length (16 bits, big-endian).
	pes = append(pes, byte(pesLength>>8), byte(pesLength&0xFF))

	// Byte 6: '10' marker(2) | scrambling(2)=00 | priority=0 | alignment=0 | copyright=0 | original=0
	pes = append(pes, 0x80)

	// Byte 7: PTS_DTS_flags(2) | other flags(6)=0
	pes = append(pes, ptsDtsFlags<<6)

	// Byte 8: PES header data length
	pes = append(pes, byte(len(headerData)))

	// Optional PTS/DTS data.
	pes = append(pes, headerData...)

	// Elementary stream payload.
	pes = append(pes, payload...)

	return pes
}

// TestPESAssembler_SinglePacket tests reassembly of a PES packet that fits
// in a single TS payload.
func TestPESAssembler_SinglePacket(t *testing.T) {
	// Build a PES packet with PTS = 90000 (1 second at 90kHz).
	esData := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	pesBytes := buildPESPacket(0xE0, 90000, -1, esData)

	assembler := &PESAssembler{}

	// Feed the PES data with payloadUnitStart=true (new PES packet).
	// No complete packet yet — this is the first data we've seen.
	result := assembler.Feed(pesBytes, true)
	if result != nil {
		t.Fatal("expected nil on first feed (no previous packet to complete)")
	}

	// Flush to get the pending packet.
	result = assembler.Flush()
	if result == nil {
		t.Fatal("expected PES packet from Flush")
	}

	if result.StreamID != 0xE0 {
		t.Errorf("expected StreamID=0xE0, got 0x%02X", result.StreamID)
	}
	if result.PTS != 90000 {
		t.Errorf("expected PTS=90000, got %d", result.PTS)
	}
	if result.DTS != 90000 {
		t.Errorf("expected DTS=90000 (same as PTS), got %d", result.DTS)
	}

	// Verify the elementary stream data.
	if len(result.Data) != len(esData) {
		t.Fatalf("expected data length=%d, got %d", len(esData), len(result.Data))
	}
	for i, b := range esData {
		if result.Data[i] != b {
			t.Errorf("data[%d]: expected 0x%02X, got 0x%02X", i, b, result.Data[i])
		}
	}
}

// TestPESAssembler_MultiPacket tests reassembly when a PES packet spans
// multiple TS packets (common for video frames).
func TestPESAssembler_MultiPacket(t *testing.T) {
	esData := make([]byte, 300) // Larger than one TS payload
	for i := range esData {
		esData[i] = byte(i & 0xFF)
	}
	pesBytes := buildPESPacket(0xE0, 180000, -1, esData)

	assembler := &PESAssembler{}

	// Split the PES data into three chunks, simulating multiple TS packets.
	chunk1 := pesBytes[:100]
	chunk2 := pesBytes[100:200]
	chunk3 := pesBytes[200:]

	// First chunk with PUSI=true (start of PES packet).
	result := assembler.Feed(chunk1, true)
	if result != nil {
		t.Fatal("unexpected result on first feed")
	}

	// Continuation chunks with PUSI=false.
	result = assembler.Feed(chunk2, false)
	if result != nil {
		t.Fatal("unexpected result on second feed")
	}

	result = assembler.Feed(chunk3, false)
	if result != nil {
		t.Fatal("unexpected result on third feed")
	}

	// Flush to get the complete packet.
	result = assembler.Flush()
	if result == nil {
		t.Fatal("expected PES packet from Flush")
	}

	if result.PTS != 180000 {
		t.Errorf("expected PTS=180000, got %d", result.PTS)
	}

	if len(result.Data) != len(esData) {
		t.Fatalf("expected data length=%d, got %d", len(esData), len(result.Data))
	}
}

// TestPESAssembler_ConsecutivePackets tests that starting a new PES packet
// completes the previous one.
func TestPESAssembler_ConsecutivePackets(t *testing.T) {
	esData1 := []byte{0xAA, 0xBB}
	esData2 := []byte{0xCC, 0xDD}

	pes1 := buildPESPacket(0xE0, 90000, -1, esData1)
	pes2 := buildPESPacket(0xE0, 180000, -1, esData2)

	assembler := &PESAssembler{}

	// Feed first PES packet.
	result := assembler.Feed(pes1, true)
	if result != nil {
		t.Fatal("unexpected result on first packet")
	}

	// Feed second PES packet — this should complete the first one.
	result = assembler.Feed(pes2, true)
	if result == nil {
		t.Fatal("expected first PES packet to be returned")
	}

	// Verify it's the first PES packet.
	if result.PTS != 90000 {
		t.Errorf("expected PTS=90000 for first packet, got %d", result.PTS)
	}
	if len(result.Data) != len(esData1) {
		t.Errorf("expected data length=%d, got %d", len(esData1), len(result.Data))
	}

	// Flush to get the second packet.
	result = assembler.Flush()
	if result == nil {
		t.Fatal("expected second PES packet from Flush")
	}
	if result.PTS != 180000 {
		t.Errorf("expected PTS=180000 for second packet, got %d", result.PTS)
	}
}

// TestPESAssembler_PTSAndDTS tests extraction of both PTS and DTS.
func TestPESAssembler_PTSAndDTS(t *testing.T) {
	esData := []byte{0x01}
	pts := int64(270000) // 3 seconds
	dts := int64(180000) // 2 seconds (decode before display)

	pesBytes := buildPESPacket(0xE0, pts, dts, esData)

	assembler := &PESAssembler{}
	assembler.Feed(pesBytes, true)

	result := assembler.Flush()
	if result == nil {
		t.Fatal("expected PES packet")
	}

	if result.PTS != pts {
		t.Errorf("expected PTS=%d, got %d", pts, result.PTS)
	}
	if result.DTS != dts {
		t.Errorf("expected DTS=%d, got %d", dts, result.DTS)
	}
}

// TestPESAssembler_NoPTS tests a PES packet with no timestamps.
func TestPESAssembler_NoPTS(t *testing.T) {
	esData := []byte{0x01, 0x02}
	pesBytes := buildPESPacket(0xC0, -1, -1, esData)

	assembler := &PESAssembler{}
	assembler.Feed(pesBytes, true)

	result := assembler.Flush()
	if result == nil {
		t.Fatal("expected PES packet")
	}

	if result.PTS != -1 {
		t.Errorf("expected PTS=-1 (no PTS), got %d", result.PTS)
	}
	if result.DTS != -1 {
		t.Errorf("expected DTS=-1 (no DTS), got %d", result.DTS)
	}
	if result.StreamID != 0xC0 {
		t.Errorf("expected StreamID=0xC0 (audio), got 0x%02X", result.StreamID)
	}
}

// TestPESAssembler_DiscardBeforeStart tests that data received before
// any PUSI=true is discarded.
func TestPESAssembler_DiscardBeforeStart(t *testing.T) {
	assembler := &PESAssembler{}

	// Feed data without PUSI — should be discarded.
	result := assembler.Feed([]byte{0x01, 0x02, 0x03}, false)
	if result != nil {
		t.Fatal("expected nil when feeding before PES start")
	}

	// Flush should return nil too.
	result = assembler.Flush()
	if result != nil {
		t.Fatal("expected nil from Flush when no PES started")
	}
}

// TestPESAssembler_EmptyPayload tests feeding empty payload.
func TestPESAssembler_EmptyPayload(t *testing.T) {
	assembler := &PESAssembler{}

	result := assembler.Feed([]byte{}, true)
	if result != nil {
		t.Fatal("expected nil for empty payload")
	}
}

// TestParsePTS_KnownValues tests the PTS encoding/parsing round-trip with
// several known timestamp values.
func TestParsePTS_KnownValues(t *testing.T) {
	tests := []struct {
		name string
		pts  int64
	}{
		{"zero", 0},
		{"one_second", 90000},
		{"one_minute", 5400000},
		{"large_value", 1000000000},
		{"max_33bit", (1 << 33) - 1}, // Maximum 33-bit value
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Encode the PTS value.
			encoded := encodePTS(0x02, tc.pts)

			// Parse it back.
			decoded := parseTimestamp(encoded)

			if decoded != tc.pts {
				t.Errorf("PTS round-trip failed: encoded %d, decoded %d", tc.pts, decoded)
			}
		})
	}
}
