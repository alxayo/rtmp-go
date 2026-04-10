package ts

import (
	"testing"
)

// buildPATPayload constructs a valid PAT section payload for testing.
// The PAT section format:
//
//	pointer_field | table_id(0x00) | section_syntax(1)|0(1)|reserved(2)|section_length(12)
//	transport_stream_id(16) | reserved(2)|version(5)|current_next(1)
//	section_number(8) | last_section_number(8)
//	[program_number(16) | reserved(3)|PID(13)] * N
//	CRC32(32)
func buildPATPayload(entries []PATEntry) []byte {
	// Calculate section length: 5 bytes (TSID through last_section_number)
	// + 4 bytes per entry + 4 bytes CRC.
	entryBytes := len(entries) * 4
	sectionLength := 5 + entryBytes + 4

	// Start with pointer field = 0 (section starts immediately).
	payload := []byte{0x00}

	// table_id = 0x00 (PAT)
	payload = append(payload, 0x00)

	// section_syntax_indicator(1) | '0'(1) | reserved(2) | section_length(12)
	// section_syntax_indicator = 1, '0' = 0, reserved = 11
	// First byte: 1_0_11_xxxx where xxxx = upper 4 bits of section_length
	payload = append(payload, 0xB0|byte((sectionLength>>8)&0x0F))
	payload = append(payload, byte(sectionLength&0xFF))

	// transport_stream_id = 0x0001
	payload = append(payload, 0x00, 0x01)

	// reserved(2)|version(5)|current_next(1) = 11_00001_1 = 0xC3
	payload = append(payload, 0xC3)

	// section_number = 0, last_section_number = 0
	payload = append(payload, 0x00, 0x00)

	// Program entries
	for _, e := range entries {
		payload = append(payload,
			byte(e.ProgramNumber>>8),
			byte(e.ProgramNumber&0xFF),
			0xE0|byte((e.PMTPID>>8)&0x1F), // reserved(3) = 111 | PID high
			byte(e.PMTPID&0xFF),            // PID low
		)
	}

	// CRC32 — we don't verify it, so just put zeros.
	payload = append(payload, 0x00, 0x00, 0x00, 0x00)

	return payload
}

// TestParsePAT_SingleProgram tests parsing a PAT with one program entry.
func TestParsePAT_SingleProgram(t *testing.T) {
	payload := buildPATPayload([]PATEntry{
		{ProgramNumber: 1, PMTPID: 0x1000},
	})

	entries, err := ParsePAT(payload)
	if err != nil {
		t.Fatalf("ParsePAT failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].ProgramNumber != 1 {
		t.Errorf("expected ProgramNumber=1, got %d", entries[0].ProgramNumber)
	}
	if entries[0].PMTPID != 0x1000 {
		t.Errorf("expected PMTPID=0x1000, got 0x%04X", entries[0].PMTPID)
	}
}

// TestParsePAT_MultiplePrograms tests parsing a PAT with multiple programs,
// including program 0 (NIT).
func TestParsePAT_MultiplePrograms(t *testing.T) {
	payload := buildPATPayload([]PATEntry{
		{ProgramNumber: 0, PMTPID: 0x0010},    // NIT
		{ProgramNumber: 1, PMTPID: 0x0100},    // Program 1
		{ProgramNumber: 2, PMTPID: 0x0200},    // Program 2
	})

	entries, err := ParsePAT(payload)
	if err != nil {
		t.Fatalf("ParsePAT failed: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Verify each entry.
	expected := []struct {
		programNum uint16
		pmtPID     uint16
	}{
		{0, 0x0010},
		{1, 0x0100},
		{2, 0x0200},
	}

	for i, e := range expected {
		if entries[i].ProgramNumber != e.programNum {
			t.Errorf("entry %d: expected ProgramNumber=%d, got %d", i, e.programNum, entries[i].ProgramNumber)
		}
		if entries[i].PMTPID != e.pmtPID {
			t.Errorf("entry %d: expected PMTPID=0x%04X, got 0x%04X", i, e.pmtPID, entries[i].PMTPID)
		}
	}
}

// TestParsePAT_InvalidTableID tests that ParsePAT rejects a payload with
// a wrong table_id.
func TestParsePAT_InvalidTableID(t *testing.T) {
	payload := buildPATPayload([]PATEntry{{ProgramNumber: 1, PMTPID: 0x100}})

	// Overwrite table_id to an invalid value.
	payload[1] = 0x02 // PMT table_id instead of PAT

	_, err := ParsePAT(payload)
	if err == nil {
		t.Fatal("expected error for invalid table_id")
	}
}

// TestParsePAT_TooShort tests that ParsePAT handles truncated data.
func TestParsePAT_TooShort(t *testing.T) {
	_, err := ParsePAT([]byte{})
	if err == nil {
		t.Fatal("expected error for empty payload")
	}

	_, err = ParsePAT([]byte{0x00, 0x00})
	if err == nil {
		t.Fatal("expected error for too-short payload")
	}
}

// buildPMTPayload constructs a valid PMT section payload for testing.
func buildPMTPayload(pcrPID uint16, streams []PMTStream) []byte {
	// Calculate sizes.
	streamBytes := 0
	for range streams {
		streamBytes += 5 // 5 bytes per stream entry (no descriptors)
	}
	// Section length = 9 (fixed fields after section_length) + streamBytes + 4 (CRC)
	sectionLength := 9 + streamBytes + 4

	// pointer_field = 0
	payload := []byte{0x00}

	// table_id = 0x02 (PMT)
	payload = append(payload, 0x02)

	// section_syntax_indicator(1)|'0'(1)|reserved(2)|section_length(12)
	payload = append(payload, 0xB0|byte((sectionLength>>8)&0x0F))
	payload = append(payload, byte(sectionLength&0xFF))

	// program_number = 0x0001
	payload = append(payload, 0x00, 0x01)

	// reserved(2)|version(5)|current_next(1) = 0xC1
	payload = append(payload, 0xC1)

	// section_number = 0, last_section_number = 0
	payload = append(payload, 0x00, 0x00)

	// reserved(3)|PCR_PID(13)
	payload = append(payload,
		0xE0|byte((pcrPID>>8)&0x1F),
		byte(pcrPID&0xFF),
	)

	// reserved(4)|program_info_length(12) = 0 (no program descriptors)
	payload = append(payload, 0xF0, 0x00)

	// Stream entries
	for _, s := range streams {
		payload = append(payload,
			s.StreamType,                    // stream_type
			0xE0|byte((s.PID>>8)&0x1F),     // reserved(3)|PID high
			byte(s.PID&0xFF),               // PID low
			0xF0,                           // reserved(4)|ES_info_length high = 0
			0x00,                           // ES_info_length low = 0
		)
	}

	// CRC32 (not verified)
	payload = append(payload, 0x00, 0x00, 0x00, 0x00)

	return payload
}

// TestParsePMT_VideoAndAudio tests parsing a PMT with H.264 video and AAC audio.
func TestParsePMT_VideoAndAudio(t *testing.T) {
	streams := []PMTStream{
		{StreamType: StreamTypeH264, PID: 0x0100},    // Video
		{StreamType: StreamTypeAAC_ADTS, PID: 0x0101}, // Audio
	}
	payload := buildPMTPayload(0x0100, streams)

	pmt, err := ParsePMT(payload)
	if err != nil {
		t.Fatalf("ParsePMT failed: %v", err)
	}

	if pmt.PCRPID != 0x0100 {
		t.Errorf("expected PCRPID=0x0100, got 0x%04X", pmt.PCRPID)
	}

	if len(pmt.Streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(pmt.Streams))
	}

	// Verify video stream.
	if pmt.Streams[0].StreamType != StreamTypeH264 {
		t.Errorf("stream 0: expected StreamType=0x1B, got 0x%02X", pmt.Streams[0].StreamType)
	}
	if pmt.Streams[0].PID != 0x0100 {
		t.Errorf("stream 0: expected PID=0x0100, got 0x%04X", pmt.Streams[0].PID)
	}

	// Verify audio stream.
	if pmt.Streams[1].StreamType != StreamTypeAAC_ADTS {
		t.Errorf("stream 1: expected StreamType=0x0F, got 0x%02X", pmt.Streams[1].StreamType)
	}
	if pmt.Streams[1].PID != 0x0101 {
		t.Errorf("stream 1: expected PID=0x0101, got 0x%04X", pmt.Streams[1].PID)
	}
}

// TestParsePMT_InvalidTableID verifies PMT parser rejects wrong table_id.
func TestParsePMT_InvalidTableID(t *testing.T) {
	payload := buildPMTPayload(0x100, []PMTStream{
		{StreamType: StreamTypeH264, PID: 0x100},
	})

	// Change table_id from 0x02 to 0x00 (PAT).
	payload[1] = 0x00

	_, err := ParsePMT(payload)
	if err == nil {
		t.Fatal("expected error for invalid table_id")
	}
}

// TestParsePMT_TooShort tests that ParsePMT handles truncated data.
func TestParsePMT_TooShort(t *testing.T) {
	_, err := ParsePMT([]byte{})
	if err == nil {
		t.Fatal("expected error for empty payload")
	}

	_, err = ParsePMT([]byte{0x00, 0x02})
	if err == nil {
		t.Fatal("expected error for too-short payload")
	}
}

// TestParsePMT_MultipleStreams tests a PMT with three different stream types.
func TestParsePMT_MultipleStreams(t *testing.T) {
	streams := []PMTStream{
		{StreamType: StreamTypeH264, PID: 0x0100},
		{StreamType: StreamTypeAAC_ADTS, PID: 0x0101},
		{StreamType: StreamTypeH265, PID: 0x0102},
	}
	payload := buildPMTPayload(0x0100, streams)

	pmt, err := ParsePMT(payload)
	if err != nil {
		t.Fatalf("ParsePMT failed: %v", err)
	}

	if len(pmt.Streams) != 3 {
		t.Fatalf("expected 3 streams, got %d", len(pmt.Streams))
	}

	for i, want := range streams {
		got := pmt.Streams[i]
		if got.StreamType != want.StreamType {
			t.Errorf("stream %d: expected StreamType=0x%02X, got 0x%02X", i, want.StreamType, got.StreamType)
		}
		if got.PID != want.PID {
			t.Errorf("stream %d: expected PID=0x%04X, got 0x%04X", i, want.PID, got.PID)
		}
	}
}
