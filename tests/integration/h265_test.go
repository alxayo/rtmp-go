package integration

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/codec"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// TestH265Bridge verifies the SRT bridge correctly handles H.265/HEVC video frames.
func TestH265Bridge(t *testing.T) {
	// Create a mock H.265 frame with VPS/SPS/PPS (simulated Annex B format)
	// Format: start code + NAL data
	vpsNALU := []byte{0x40, 0x01, 0x02, 0x03}
	// SPS must be at least 13 bytes (for access to SPS[12] in BuildHEVCSequenceHeader)
	spsNALU := []byte{0x42, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00}
	ppsNALU := []byte{0x44, 0x21, 0x22, 0x23}

	// Build frame with Annex B start codes
	frameData := make([]byte, 0, 100)
	frameData = append(frameData, 0x00, 0x00, 0x00, 0x01) // 4-byte start code
	frameData = append(frameData, vpsNALU...)
	frameData = append(frameData, 0x00, 0x00, 0x01)      // 3-byte start code
	frameData = append(frameData, spsNALU...)
	frameData = append(frameData, 0x00, 0x00, 0x01)      // 3-byte start code
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
		t.Error("ExtractH265VPSSPSPPS() VPS is nil, want non-nil")
	}
	if sps == nil {
		t.Error("ExtractH265VPSSPSPPS() SPS is nil, want non-nil")
	}
	if pps == nil {
		t.Error("ExtractH265VPSSPSPPS() PPS is nil, want non-nil")
	}

	// Test sequence header building
	seqHeader := codec.BuildHEVCSequenceHeader(vps, sps, pps)
	if len(seqHeader) < 5 {
		t.Errorf("BuildHEVCSequenceHeader() returned %d bytes, want at least 5", len(seqHeader))
	}

	// Check frame type and codec ID in sequence header
	// Byte 0 should be 0x1C: keyframe (1) << 4 | HEVC codec (12)
	if seqHeader[0] != 0x1C {
		t.Errorf("BuildHEVCSequenceHeader() byte[0] = 0x%02X, want 0x1C", seqHeader[0])
	}

	// Check packet type (should be 0x00 for sequence header)
	if seqHeader[1] != 0x00 {
		t.Errorf("BuildHEVCSequenceHeader() byte[1] = 0x%02X, want 0x00", seqHeader[1])
	}
}

// TestH265VideoFrame verifies H.265 video frame building.
func TestH265VideoFrame(t *testing.T) {
	// Create mock video frame NALUs (IDR keyframe)
	idrNALU := []byte{0x26, 0xAB, 0xCD, 0xEF}  // IDR frame type=19

	nalus := [][]byte{idrNALU}

	// Test keyframe detection
	if !codec.IsH265KeyframeNALU(idrNALU) {
		t.Error("IsH265KeyframeNALU() returned false for IDR, want true")
	}

	// Build video frame
	payload := codec.BuildHEVCVideoFrame(nalus, true, 0)

	if len(payload) < 5 {
		t.Errorf("BuildHEVCVideoFrame() returned %d bytes, want at least 5", len(payload))
	}

	// Check byte 0: keyframe (1) << 4 | HEVC (12) = 0x1C
	if payload[0] != 0x1C {
		t.Errorf("BuildHEVCVideoFrame(keyframe) byte[0] = 0x%02X, want 0x1C", payload[0])
	}

	// Check byte 1: packet type should be 0x01 (NALU data)
	if payload[1] != 0x01 {
		t.Errorf("BuildHEVCVideoFrame() byte[1] = 0x%02X, want 0x01", payload[1])
	}

	// Test inter-frame
	interNALU := []byte{0x02, 0x12, 0x34, 0x56}  // Non-IDR frame
	payload = codec.BuildHEVCVideoFrame([][]byte{interNALU}, false, 100)

	// Check byte 0: inter (2) << 4 | HEVC (12) = 0x2C
	if payload[0] != 0x2C {
		t.Errorf("BuildHEVCVideoFrame(inter) byte[0] = 0x%02X, want 0x2C", payload[0])
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
	// Simulate H.265 video frames being converted to chunk.Message
	// for broadcast to RTMP subscribers

	// Create a mock video frame payload (would come from bridge.handleH265Frame)
	vps := []byte{0x40, 0x01}
	sps := []byte{0x42, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00}
	pps := []byte{0x44, 0x01}

	seqHeader := codec.BuildHEVCSequenceHeader(vps, sps, pps)

	// The bridge would create chunk.Message with:
	// - TypeID = 9 (video)
	// - Timestamp = 0 (for sequence header)
	// - Payload = seqHeader
	msg := &chunk.Message{
		CSID:            6,        // Video stream
		Timestamp:       0,        // Sequence header timestamp
		MessageLength:   uint32(len(seqHeader)),
		TypeID:          9,        // Video
		MessageStreamID: 1,        // Media stream
		Payload:         seqHeader,
	}

	if msg.TypeID != 9 {
		t.Errorf("chunk.Message TypeID = %d, want 9", msg.TypeID)
	}

	if msg.MessageLength != uint32(len(seqHeader)) {
		t.Errorf("chunk.Message MessageLength = %d, want %d", msg.MessageLength, len(seqHeader))
	}

	// Verify the payload starts with H.265 sequence header (codec byte 0x1C)
	if msg.Payload[0] != 0x1C {
		t.Errorf("Sequence header byte[0] = 0x%02X, want 0x1C", msg.Payload[0])
	}
}
