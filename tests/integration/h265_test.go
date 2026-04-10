package integration

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/codec"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// TestH265Bridge verifies the SRT bridge correctly handles H.265/HEVC video frames.
func TestH265Bridge(t *testing.T) {
	// Create mock H.265 VPS/SPS/PPS NALUs
	vpsNALU := []byte{0x40, 0x01, 0x02, 0x03}

	// SPS must be at least 15 bytes for profile_tier_level extraction
	// Layout: NAL header (2B) + VPS/layers byte + profile_tier_level (12B)
	spsNALU := []byte{
		0x42, 0x01, // NAL header: type=33 (SPS)
		0x01,       // vps_id=0, max_sub_layers=0, temporal_nesting=1
		0x01,       // profile_space=0, tier=0, profile_idc=1 (Main)
		0x60, 0x00, 0x00, 0x00, // profile_compat_flags
		0xB0, 0x00, 0x00, 0x00, 0x00, 0x00, // constraint_indicator_flags
		0x5D, // level_idc = 93 (Level 3.1)
	}
	ppsNALU := []byte{0x44, 0x21, 0x22, 0x23}

	// Build frame with Annex B start codes
	frameData := make([]byte, 0, 100)
	frameData = append(frameData, 0x00, 0x00, 0x00, 0x01) // 4-byte start code
	frameData = append(frameData, vpsNALU...)
	frameData = append(frameData, 0x00, 0x00, 0x01) // 3-byte start code
	frameData = append(frameData, spsNALU...)
	frameData = append(frameData, 0x00, 0x00, 0x01) // 3-byte start code
	frameData = append(frameData, ppsNALU...)

	// Test NAL unit type extraction
	if got := codec.H265NALUType(vpsNALU); got != 32 {
		t.Errorf("H265NALUType(VPS) = %d, want 32", got)
	}
	if got := codec.H265NALUType(spsNALU); got != 33 {
		t.Errorf("H265NALUType(SPS) = %d, want 33", got)
	}
	if got := codec.H265NALUType(ppsNALU); got != 34 {
		t.Errorf("H265NALUType(PPS) = %d, want 34", got)
	}

	// Test Annex B splitting
	nalus := codec.SplitH265AnnexB(frameData)
	if len(nalus) < 3 {
		t.Errorf("SplitH265AnnexB() returned %d NALUs, want at least 3", len(nalus))
	}

	// Test VPS/SPS/PPS extraction
	vps, sps, pps, found := codec.ExtractH265VPSSPSPPS(nalus)
	if !found {
		t.Error("ExtractH265VPSSPSPPS() found=false, want true")
	}
	if vps == nil {
		t.Error("ExtractH265VPSSPSPPS() VPS is nil")
	}
	if sps == nil {
		t.Error("ExtractH265VPSSPSPPS() SPS is nil")
	}
	if pps == nil {
		t.Error("ExtractH265VPSSPSPPS() PPS is nil")
	}

	// Test sequence header building (Enhanced RTMP format)
	seqHeader := codec.BuildHEVCSequenceHeader(vps, sps, pps)
	if len(seqHeader) < 5 {
		t.Errorf("BuildHEVCSequenceHeader() returned %d bytes, want at least 5", len(seqHeader))
	}

	// Byte 0: Enhanced RTMP = 0x90 (IsExHeader=1, Keyframe=1, SequenceStart=0)
	if seqHeader[0] != 0x90 {
		t.Errorf("seqHeader byte[0] = 0x%02X, want 0x90", seqHeader[0])
	}

	// Bytes 1-4: FourCC = "hvc1"
	if string(seqHeader[1:5]) != "hvc1" {
		t.Errorf("seqHeader FourCC = %q, want \"hvc1\"", string(seqHeader[1:5]))
	}
}

// TestH265VideoFrame verifies H.265 video frame building.
func TestH265VideoFrame(t *testing.T) {
	// Create mock video frame NALUs (IDR keyframe)
	idrNALU := []byte{0x26, 0xAB, 0xCD, 0xEF} // IDR frame type=19

	nalus := [][]byte{idrNALU}

	// Test keyframe detection
	if !codec.IsH265KeyframeNALU(idrNALU) {
		t.Error("IsH265KeyframeNALU() returned false for IDR, want true")
	}

	// Build video frame (Enhanced RTMP format)
	payload := codec.BuildHEVCVideoFrame(nalus, true, 0)

	// Enhanced RTMP: 1 + 4 (FourCC) + 3 (CTS) + AVCC data = 8+ bytes
	if len(payload) < 8 {
		t.Errorf("BuildHEVCVideoFrame() returned %d bytes, want at least 8", len(payload))
	}

	// Byte 0: Enhanced RTMP keyframe = 0x91
	if payload[0] != 0x91 {
		t.Errorf("BuildHEVCVideoFrame(keyframe) byte[0] = 0x%02X, want 0x91", payload[0])
	}

	// Bytes 1-4: FourCC = "hvc1"
	if string(payload[1:5]) != "hvc1" {
		t.Errorf("FourCC = %q, want \"hvc1\"", string(payload[1:5]))
	}

	// Test inter-frame
	interNALU := []byte{0x02, 0x12, 0x34, 0x56} // Non-IDR frame
	payload = codec.BuildHEVCVideoFrame([][]byte{interNALU}, false, 100)

	// Byte 0: Enhanced RTMP inter-frame = 0xA1
	if payload[0] != 0xA1 {
		t.Errorf("BuildHEVCVideoFrame(inter) byte[0] = 0x%02X, want 0xA1", payload[0])
	}
}

// TestH265ParameterSetDetection verifies H.265 parameter set identification.
func TestH265ParameterSetDetection(t *testing.T) {
	tests := []struct {
		name              string
		naluByte          byte
		wantIsParamSet    bool
		wantIsKeyframe    bool
	}{
		// Parameter sets
		{"vps", 0x40, true, false},
		{"sps", 0x42, true, false},
		{"pps", 0x44, true, false},

		// Keyframes (IDR types 16-21)
		{"idr_16", 0x20, false, true},
		{"idr_19", 0x26, false, true},
		{"idr_21", 0x2A, false, true},

		// Non-keyframes
		{"inter_1", 0x02, false, false},
		{"inter_15", 0x1E, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nalu := []byte{tc.naluByte}

			got := codec.IsH265ParameterSet(nalu)
			if got != tc.wantIsParamSet {
				t.Errorf("IsH265ParameterSet(0x%02X) = %v, want %v", tc.naluByte, got, tc.wantIsParamSet)
			}

			got = codec.IsH265KeyframeNALU(nalu)
			if got != tc.wantIsKeyframe {
				t.Errorf("IsH265KeyframeNALU(0x%02X) = %v, want %v", tc.naluByte, got, tc.wantIsKeyframe)
			}
		})
	}
}

// TestH265ChunkMessageCreation verifies that H.265 frames are properly wrapped
// in RTMP chunk.Message for broadcast to subscribers.
func TestH265ChunkMessageCreation(t *testing.T) {
	// Create mock parameter sets
	vps := []byte{0x40, 0x01}
	// SPS with 15+ bytes for proper profile extraction
	sps := []byte{
		0x42, 0x01, // NAL header
		0x01,                                  // vps_id/layers
		0x01,                                  // profile_idc=1 (Main)
		0x60, 0x00, 0x00, 0x00,              // profile_compat
		0xB0, 0x00, 0x00, 0x00, 0x00, 0x00, // constraint_flags
		0x5D,                                  // level_idc
	}
	pps := []byte{0x44, 0x01}

	seqHeader := codec.BuildHEVCSequenceHeader(vps, sps, pps)

	// The bridge creates chunk.Message with TypeID=9 (video)
	msg := &chunk.Message{
		CSID:            6,
		Timestamp:       0,
		MessageLength:   uint32(len(seqHeader)),
		TypeID:          9,
		MessageStreamID: 1,
		Payload:         seqHeader,
	}

	if msg.TypeID != 9 {
		t.Errorf("chunk.Message TypeID = %d, want 9", msg.TypeID)
	}

	if msg.MessageLength != uint32(len(seqHeader)) {
		t.Errorf("chunk.Message MessageLength = %d, want %d", msg.MessageLength, len(seqHeader))
	}

	// Verify Enhanced RTMP header: 0x90 (SequenceStart) + "hvc1"
	if msg.Payload[0] != 0x90 {
		t.Errorf("Sequence header byte[0] = 0x%02X, want 0x90", msg.Payload[0])
	}
	if string(msg.Payload[1:5]) != "hvc1" {
		t.Errorf("FourCC = %q, want \"hvc1\"", string(msg.Payload[1:5]))
	}
}
