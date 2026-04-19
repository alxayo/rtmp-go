package media

import "testing"

// TestParseMultitrack_OneTrack verifies parsing a single-track multitrack message.
// This is used when the track ID is != 0 (explicit track identification).
func TestParseMultitrack_OneTrack(t *testing.T) {
	// byte 0: multitrackType=0 (OneTrack) in high nibble,
	//         innerPktType=1 (CodedFrames) in low nibble → 0x01
	// byte 1: trackId=3
	// bytes 2+: track data
	data := []byte{0x01, 0x03, 0xAA, 0xBB, 0xCC}
	msg, err := ParseMultitrack(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if msg.MultitrackType != MultitrackTypeOneTrack {
		_tFatalf(t, "multitrack type mismatch: got %d, want %d", msg.MultitrackType, MultitrackTypeOneTrack)
	}
	if msg.InnerPacketType != 1 {
		_tFatalf(t, "inner packet type mismatch: got %d, want 1", msg.InnerPacketType)
	}
	if len(msg.Tracks) != 1 {
		_tFatalf(t, "expected 1 track, got %d", len(msg.Tracks))
	}
	if msg.Tracks[0].TrackID != 3 {
		_tFatalf(t, "track ID mismatch: got %d, want 3", msg.Tracks[0].TrackID)
	}
	if len(msg.Tracks[0].Data) != 3 || msg.Tracks[0].Data[0] != 0xAA {
		_tFatalf(t, "track data mismatch: %v", msg.Tracks[0].Data)
	}
}

// TestParseMultitrack_ManyTracks verifies parsing multiple tracks with the same codec.
// Each track has: [trackId(1B)][dataLen(3B)][data(N bytes)]
func TestParseMultitrack_ManyTracks(t *testing.T) {
	// byte 0: multitrackType=1 (ManyTracks), innerPktType=0 (SequenceStart) → 0x10
	// Track 0: id=0, len=2 (0x000002), data=[0x11, 0x22]
	// Track 1: id=1, len=3 (0x000003), data=[0x33, 0x44, 0x55]
	data := []byte{
		0x10,                   // header: ManyTracks + SequenceStart
		0x00, 0x00, 0x00, 0x02, // track 0: id=0, len=2
		0x11, 0x22, // track 0 data
		0x01, 0x00, 0x00, 0x03, // track 1: id=1, len=3
		0x33, 0x44, 0x55, // track 1 data
	}
	msg, err := ParseMultitrack(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if msg.MultitrackType != MultitrackTypeManyTracks {
		_tFatalf(t, "multitrack type mismatch: got %d, want %d", msg.MultitrackType, MultitrackTypeManyTracks)
	}
	if msg.InnerPacketType != 0 {
		_tFatalf(t, "inner packet type mismatch: got %d, want 0", msg.InnerPacketType)
	}
	if len(msg.Tracks) != 2 {
		_tFatalf(t, "expected 2 tracks, got %d", len(msg.Tracks))
	}
	// Verify track 0
	if msg.Tracks[0].TrackID != 0 || len(msg.Tracks[0].Data) != 2 {
		_tFatalf(t, "track 0 mismatch: id=%d, data=%v", msg.Tracks[0].TrackID, msg.Tracks[0].Data)
	}
	// Verify track 1
	if msg.Tracks[1].TrackID != 1 || len(msg.Tracks[1].Data) != 3 {
		_tFatalf(t, "track 1 mismatch: id=%d, data=%v", msg.Tracks[1].TrackID, msg.Tracks[1].Data)
	}
	if msg.Tracks[1].Data[0] != 0x33 {
		_tFatalf(t, "track 1 data[0] mismatch: got 0x%02X, want 0x33", msg.Tracks[1].Data[0])
	}
}

// TestParseMultitrack_ManyTracksManyCodecs verifies parsing multiple tracks
// where each track has its own FourCC codec identifier.
// Each track has: [trackId(1B)][FourCC(4B)][dataLen(3B)][data(N bytes)]
func TestParseMultitrack_ManyTracksManyCodecs(t *testing.T) {
	// byte 0: multitrackType=2 (ManyTracksManyCodecs), innerPktType=0 → 0x20
	// Track 0: id=0, fourcc="hvc1", len=2 (0x000002), data=[0xAA, 0xBB]
	// Track 1: id=1, fourcc="av01", len=1 (0x000001), data=[0xCC]
	data := []byte{
		0x20,                               // header: ManyTracksManyCodecs + SequenceStart
		0x00,                               // track 0: id=0
		'h', 'v', 'c', '1',                // track 0: fourcc="hvc1"
		0x00, 0x00, 0x02,                   // track 0: len=2
		0xAA, 0xBB,                         // track 0: data
		0x01,                               // track 1: id=1
		'a', 'v', '0', '1',                // track 1: fourcc="av01"
		0x00, 0x00, 0x01,                   // track 1: len=1
		0xCC,                               // track 1: data
	}
	msg, err := ParseMultitrack(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if msg.MultitrackType != MultitrackTypeManyTracksManyCodecs {
		_tFatalf(t, "multitrack type mismatch: got %d", msg.MultitrackType)
	}
	if len(msg.Tracks) != 2 {
		_tFatalf(t, "expected 2 tracks, got %d", len(msg.Tracks))
	}
	// Verify track 0
	if msg.Tracks[0].TrackID != 0 || msg.Tracks[0].FourCC != "hvc1" {
		_tFatalf(t, "track 0 mismatch: id=%d, fourcc=%s", msg.Tracks[0].TrackID, msg.Tracks[0].FourCC)
	}
	if len(msg.Tracks[0].Data) != 2 || msg.Tracks[0].Data[0] != 0xAA {
		_tFatalf(t, "track 0 data mismatch: %v", msg.Tracks[0].Data)
	}
	// Verify track 1
	if msg.Tracks[1].TrackID != 1 || msg.Tracks[1].FourCC != "av01" {
		_tFatalf(t, "track 1 mismatch: id=%d, fourcc=%s", msg.Tracks[1].TrackID, msg.Tracks[1].FourCC)
	}
	if len(msg.Tracks[1].Data) != 1 || msg.Tracks[1].Data[0] != 0xCC {
		_tFatalf(t, "track 1 data mismatch: %v", msg.Tracks[1].Data)
	}
}

// TestParseMultitrack_TooShort verifies that input shorter than 2 bytes is rejected.
func TestParseMultitrack_TooShort(t *testing.T) {
	_, err := ParseMultitrack([]byte{0x01})
	if err == nil {
		t.Fatal("expected error for too-short data")
	}
}

// TestParseMultitrack_Empty verifies that empty input is rejected.
func TestParseMultitrack_Empty(t *testing.T) {
	_, err := ParseMultitrack([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

// TestParseMultitrack_UnknownType verifies that an unknown multitrack type
// returns an error (types 3+ are not defined in the spec).
func TestParseMultitrack_UnknownType(t *testing.T) {
	// multitrackType=3 (unknown), innerPktType=0
	_, err := ParseMultitrack([]byte{0x30, 0x00})
	if err == nil {
		t.Fatal("expected error for unknown multitrack type")
	}
}

// TestParseMultitrack_OneTrackNoData verifies a OneTrack message with
// a track ID but no trailing track data.
func TestParseMultitrack_OneTrackNoData(t *testing.T) {
	data := []byte{0x00, 0x05} // OneTrack, innerPktType=0, trackId=5, no data
	msg, err := ParseMultitrack(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if len(msg.Tracks) != 1 {
		_tFatalf(t, "expected 1 track, got %d", len(msg.Tracks))
	}
	if msg.Tracks[0].TrackID != 5 {
		_tFatalf(t, "track ID mismatch: got %d, want 5", msg.Tracks[0].TrackID)
	}
	if len(msg.Tracks[0].Data) != 0 {
		_tFatalf(t, "expected empty track data, got %v", msg.Tracks[0].Data)
	}
}
