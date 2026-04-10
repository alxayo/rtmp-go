package codec

import (
	"testing"
)

// TestBuildAVCSequenceHeader tests the RTMP video sequence header construction.
func TestBuildAVCSequenceHeader(t *testing.T) {
	// Minimal SPS: profile=66(Baseline), compat=0xC0, level=30
	sps := []byte{0x67, 0x42, 0xC0, 0x1E, 0xD9, 0x00}
	pps := []byte{0x68, 0xCE, 0x38, 0x80}

	header := BuildAVCSequenceHeader(sps, pps)

	// Verify RTMP video tag header
	if header[0] != 0x17 {
		t.Errorf("byte 0: got 0x%02X, want 0x17 (keyframe + AVC)", header[0])
	}
	if header[1] != 0x00 {
		t.Errorf("byte 1: got 0x%02X, want 0x00 (sequence header)", header[1])
	}
	// CTS should be 0
	if header[2] != 0 || header[3] != 0 || header[4] != 0 {
		t.Errorf("CTS: got [%02X %02X %02X], want [00 00 00]", header[2], header[3], header[4])
	}

	// Verify AVCDecoderConfigurationRecord
	off := 5
	if header[off] != 1 {
		t.Errorf("config version: got %d, want 1", header[off])
	}
	if header[off+1] != 0x42 {
		t.Errorf("profile: got 0x%02X, want 0x42 (Baseline)", header[off+1])
	}
	if header[off+2] != 0xC0 {
		t.Errorf("compat: got 0x%02X, want 0xC0", header[off+2])
	}
	if header[off+3] != 0x1E {
		t.Errorf("level: got 0x%02X, want 0x1E (3.0)", header[off+3])
	}
	if header[off+4] != 0xFF {
		t.Errorf("lengthSizeMinusOne: got 0x%02X, want 0xFF", header[off+4])
	}
	if header[off+5] != 0xE1 {
		t.Errorf("numSPS: got 0x%02X, want 0xE1 (1 SPS)", header[off+5])
	}
}

// TestBuildAVCVideoFrame_Keyframe tests building a keyframe video tag.
func TestBuildAVCVideoFrame_Keyframe(t *testing.T) {
	nalus := [][]byte{{0x65, 0xAA, 0xBB}} // IDR NALU

	frame := BuildAVCVideoFrame(nalus, true, 0)

	if frame[0] != 0x17 {
		t.Errorf("byte 0: got 0x%02X, want 0x17 (keyframe + AVC)", frame[0])
	}
	if frame[1] != 0x01 {
		t.Errorf("byte 1: got 0x%02X, want 0x01 (NALU)", frame[1])
	}
}

// TestBuildAVCVideoFrame_InterFrame tests building an inter-frame video tag.
func TestBuildAVCVideoFrame_InterFrame(t *testing.T) {
	nalus := [][]byte{{0x41, 0xCC}} // Non-IDR slice

	frame := BuildAVCVideoFrame(nalus, false, 33) // 33ms CTS

	if frame[0] != 0x27 {
		t.Errorf("byte 0: got 0x%02X, want 0x27 (inter-frame + AVC)", frame[0])
	}

	// Check CTS encoding (33 = 0x000021)
	cts := int32(frame[2])<<16 | int32(frame[3])<<8 | int32(frame[4])
	if cts != 33 {
		t.Errorf("CTS: got %d, want 33", cts)
	}
}

// TestBuildAVCVideoFrame_AVCC tests that NALUs are properly AVCC-encoded.
func TestBuildAVCVideoFrame_AVCC(t *testing.T) {
	nalu := []byte{0x65, 0xAA, 0xBB, 0xCC} // 4 bytes

	frame := BuildAVCVideoFrame([][]byte{nalu}, true, 0)

	// After 5-byte header, expect 4-byte length prefix + NALU data
	// Length = 4 → [00 00 00 04]
	if frame[5] != 0 || frame[6] != 0 || frame[7] != 0 || frame[8] != 4 {
		t.Errorf("AVCC length: got [%02X %02X %02X %02X], want [00 00 00 04]",
			frame[5], frame[6], frame[7], frame[8])
	}
	if frame[9] != 0x65 {
		t.Errorf("NALU data: got 0x%02X, want 0x65", frame[9])
	}
}
