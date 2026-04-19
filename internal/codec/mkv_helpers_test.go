package codec

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestParseAVCDecoderConfig validates parsing of AVCDecoderConfigurationRecord
// from MKV CodecPrivate data.
func TestParseAVCDecoderConfig(t *testing.T) {
	t.Run("valid config with 4-byte NALU lengths", func(t *testing.T) {
		// Build a minimal AVCDecoderConfigurationRecord:
		//   Byte 0: configVersion=1
		//   Byte 1: profile=66 (Baseline)
		//   Byte 2: compatibility=0xC0
		//   Byte 3: level=30
		//   Byte 4: 0xFF (reserved=0xFC | lengthSizeMinusOne=3 → 4-byte lengths)
		//   Byte 5: 0xE1 (reserved=0xE0 | numSPS=1)
		//   SPS: length=4, data=[0x67, 0x42, 0xC0, 0x1E]
		//   numPPS: 1
		//   PPS: length=3, data=[0x68, 0xCE, 0x38]
		sps := []byte{0x67, 0x42, 0xC0, 0x1E}
		pps := []byte{0x68, 0xCE, 0x38}

		var buf []byte
		buf = append(buf, 1, 66, 0xC0, 30) // config version, profile, compat, level
		buf = append(buf, 0xFF)              // 0xFC | 3 (4-byte NALU lengths)
		buf = append(buf, 0xE1)              // 0xE0 | 1 SPS

		// SPS entry: 2-byte length + data
		lenBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBuf, uint16(len(sps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, sps...)

		// PPS count + entry
		buf = append(buf, 1) // 1 PPS
		binary.BigEndian.PutUint16(lenBuf, uint16(len(pps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, pps...)

		config, err := ParseAVCDecoderConfig(buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.NALULengthSize != 4 {
			t.Errorf("NALULengthSize = %d, want 4", config.NALULengthSize)
		}
		if !bytes.Equal(config.SPS, sps) {
			t.Errorf("SPS = %x, want %x", config.SPS, sps)
		}
		if !bytes.Equal(config.PPS, pps) {
			t.Errorf("PPS = %x, want %x", config.PPS, pps)
		}
	})

	t.Run("valid config with 2-byte NALU lengths", func(t *testing.T) {
		sps := []byte{0x67, 0x64}
		pps := []byte{0x68}

		var buf []byte
		buf = append(buf, 1, 100, 0, 40) // High profile, level 4.0
		buf = append(buf, 0xFD)           // 0xFC | 1 → 2-byte NALU lengths
		buf = append(buf, 0xE1)           // 1 SPS

		lenBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBuf, uint16(len(sps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, sps...)

		buf = append(buf, 1)
		binary.BigEndian.PutUint16(lenBuf, uint16(len(pps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, pps...)

		config, err := ParseAVCDecoderConfig(buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.NALULengthSize != 2 {
			t.Errorf("NALULengthSize = %d, want 2", config.NALULengthSize)
		}
	})

	t.Run("too short", func(t *testing.T) {
		_, err := ParseAVCDecoderConfig([]byte{1, 2, 3})
		if err == nil {
			t.Fatal("expected error for short config")
		}
	})

	t.Run("wrong version", func(t *testing.T) {
		buf := make([]byte, 20)
		buf[0] = 2 // Version 2 is not supported
		_, err := ParseAVCDecoderConfig(buf)
		if err == nil {
			t.Fatal("expected error for wrong version")
		}
	})

	t.Run("no SPS entries", func(t *testing.T) {
		buf := []byte{1, 66, 0, 30, 0xFF, 0xE0} // numSPS=0
		_, err := ParseAVCDecoderConfig(buf)
		if err == nil {
			t.Fatal("expected error for no SPS")
		}
	})

	t.Run("truncated SPS data", func(t *testing.T) {
		buf := []byte{1, 66, 0, 30, 0xFF, 0xE1, 0x00, 0x10} // SPS length=16 but no data
		_, err := ParseAVCDecoderConfig(buf)
		if err == nil {
			t.Fatal("expected error for truncated SPS")
		}
	})

	t.Run("SPS data is copied", func(t *testing.T) {
		sps := []byte{0x67, 0x42, 0xC0, 0x1E}
		pps := []byte{0x68}

		var buf []byte
		buf = append(buf, 1, 66, 0xC0, 30, 0xFF, 0xE1)
		lenBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBuf, uint16(len(sps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, sps...)
		buf = append(buf, 1)
		binary.BigEndian.PutUint16(lenBuf, uint16(len(pps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, pps...)

		config, _ := ParseAVCDecoderConfig(buf)

		// Modify original buffer — SPS should not change
		buf[8] = 0xFF
		if config.SPS[1] == 0xFF {
			t.Error("SPS was not copied — points into original data")
		}
	})
}

// TestSplitLengthPrefixed validates splitting of AVCC-format NALUs.
func TestSplitLengthPrefixed(t *testing.T) {
	t.Run("4-byte length prefix", func(t *testing.T) {
		// Two NALUs: [4-byte len=3][AAA] [4-byte len=2][BB]
		var buf []byte
		lenBuf := make([]byte, 4)

		binary.BigEndian.PutUint32(lenBuf, 3)
		buf = append(buf, lenBuf...)
		buf = append(buf, 'A', 'A', 'A')

		binary.BigEndian.PutUint32(lenBuf, 2)
		buf = append(buf, lenBuf...)
		buf = append(buf, 'B', 'B')

		nalus := SplitLengthPrefixed(buf, 4)
		if len(nalus) != 2 {
			t.Fatalf("got %d NALUs, want 2", len(nalus))
		}
		if !bytes.Equal(nalus[0], []byte("AAA")) {
			t.Errorf("NALU[0] = %q, want AAA", nalus[0])
		}
		if !bytes.Equal(nalus[1], []byte("BB")) {
			t.Errorf("NALU[1] = %q, want BB", nalus[1])
		}
	})

	t.Run("2-byte length prefix", func(t *testing.T) {
		var buf []byte
		lenBuf := make([]byte, 2)

		binary.BigEndian.PutUint16(lenBuf, 5)
		buf = append(buf, lenBuf...)
		buf = append(buf, 'H', 'E', 'L', 'L', 'O')

		nalus := SplitLengthPrefixed(buf, 2)
		if len(nalus) != 1 {
			t.Fatalf("got %d NALUs, want 1", len(nalus))
		}
		if !bytes.Equal(nalus[0], []byte("HELLO")) {
			t.Errorf("NALU[0] = %q, want HELLO", nalus[0])
		}
	})

	t.Run("1-byte length prefix", func(t *testing.T) {
		buf := []byte{2, 'A', 'B', 3, 'C', 'D', 'E'}
		nalus := SplitLengthPrefixed(buf, 1)
		if len(nalus) != 2 {
			t.Fatalf("got %d NALUs, want 2", len(nalus))
		}
	})

	t.Run("truncated data stops cleanly", func(t *testing.T) {
		// 4-byte length says 100 but only 3 bytes follow
		var buf []byte
		lenBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(lenBuf, 100)
		buf = append(buf, lenBuf...)
		buf = append(buf, 'A', 'B', 'C')

		nalus := SplitLengthPrefixed(buf, 4)
		if len(nalus) != 0 {
			t.Errorf("got %d NALUs from truncated data, want 0", len(nalus))
		}
	})

	t.Run("empty data returns nil", func(t *testing.T) {
		nalus := SplitLengthPrefixed(nil, 4)
		if nalus != nil {
			t.Errorf("got %d NALUs from nil data", len(nalus))
		}
	})

	t.Run("zero-length NALU stops parsing", func(t *testing.T) {
		// [4-byte len=0] causes SplitLengthPrefixed to break (zero is invalid)
		// This is correct behavior — zero-length NALUs shouldn't appear
		var buf []byte
		lenBuf := make([]byte, 4)

		binary.BigEndian.PutUint32(lenBuf, 0)
		buf = append(buf, lenBuf...)

		binary.BigEndian.PutUint32(lenBuf, 1)
		buf = append(buf, lenBuf...)
		buf = append(buf, 'X')

		nalus := SplitLengthPrefixed(buf, 4)
		if len(nalus) != 0 {
			t.Errorf("got %d NALUs, want 0 (zero-length stops parsing)", len(nalus))
		}
	})
}

// TestParseHEVCDecoderConfig validates parsing of HEVCDecoderConfigurationRecord.
func TestParseHEVCDecoderConfig(t *testing.T) {
	// Helper to build a minimal HEVCDecoderConfigurationRecord
	buildHEVCConfig := func(vps, sps, pps []byte, naluLenSize int) []byte {
		// 23 bytes fixed header
		buf := make([]byte, 23)
		buf[0] = 1                              // configurationVersion
		buf[21] = 0xFC | byte(naluLenSize-1)    // lengthSizeMinusOne
		buf[22] = 3                              // numOfArrays (VPS, SPS, PPS)

		// VPS array
		buf = append(buf, 0xA0) // array_completeness=1 | nal_unit_type=32(VPS)
		lenBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBuf, 1) // 1 NALU
		buf = append(buf, lenBuf...)
		binary.BigEndian.PutUint16(lenBuf, uint16(len(vps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, vps...)

		// SPS array
		buf = append(buf, 0xA1) // nal_unit_type=33(SPS)
		binary.BigEndian.PutUint16(lenBuf, 1)
		buf = append(buf, lenBuf...)
		binary.BigEndian.PutUint16(lenBuf, uint16(len(sps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, sps...)

		// PPS array
		buf = append(buf, 0xA2) // nal_unit_type=34(PPS)
		binary.BigEndian.PutUint16(lenBuf, 1)
		buf = append(buf, lenBuf...)
		binary.BigEndian.PutUint16(lenBuf, uint16(len(pps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, pps...)

		return buf
	}

	t.Run("valid config with all parameter sets", func(t *testing.T) {
		vps := []byte{0x40, 0x01, 0x0C, 0x06}
		sps := []byte{0x42, 0x01, 0x01, 0x01, 0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x5D}
		pps := []byte{0x44, 0x01, 0xC0}

		data := buildHEVCConfig(vps, sps, pps, 4)
		config, err := ParseHEVCDecoderConfig(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.NALULengthSize != 4 {
			t.Errorf("NALULengthSize = %d, want 4", config.NALULengthSize)
		}
		if !bytes.Equal(config.VPS, vps) {
			t.Errorf("VPS = %x, want %x", config.VPS, vps)
		}
		if !bytes.Equal(config.SPS, sps) {
			t.Errorf("SPS = %x, want %x", config.SPS, sps)
		}
		if !bytes.Equal(config.PPS, pps) {
			t.Errorf("PPS = %x, want %x", config.PPS, pps)
		}
	})

	t.Run("too short", func(t *testing.T) {
		_, err := ParseHEVCDecoderConfig(make([]byte, 10))
		if err == nil {
			t.Fatal("expected error for short config")
		}
	})

	t.Run("wrong version", func(t *testing.T) {
		buf := make([]byte, 23)
		buf[0] = 2 // Wrong version
		_, err := ParseHEVCDecoderConfig(buf)
		if err == nil {
			t.Fatal("expected error for wrong version")
		}
	})

	t.Run("missing SPS", func(t *testing.T) {
		// Build config with only VPS array
		buf := make([]byte, 23)
		buf[0] = 1
		buf[21] = 0xFF
		buf[22] = 1 // Only 1 array

		vps := []byte{0x40, 0x01}
		buf = append(buf, 0xA0) // VPS
		lenBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBuf, 1)
		buf = append(buf, lenBuf...)
		binary.BigEndian.PutUint16(lenBuf, uint16(len(vps)))
		buf = append(buf, lenBuf...)
		buf = append(buf, vps...)

		_, err := ParseHEVCDecoderConfig(buf)
		if err == nil {
			t.Fatal("expected error for missing SPS")
		}
	})

	t.Run("parameter sets are copied", func(t *testing.T) {
		vps := []byte{0x40, 0x01}
		sps := []byte{0x42, 0x01}
		pps := []byte{0x44, 0x01}

		data := buildHEVCConfig(vps, sps, pps, 4)
		config, _ := ParseHEVCDecoderConfig(data)

		// Modify original data — config VPS should not change
		data[23+1+2+2] = 0xFF // First byte of VPS data
		if config.VPS[0] == 0xFF {
			t.Error("VPS was not copied — points into original data")
		}
	})
}

// TestBuildAACSequenceHeaderFromConfig validates wrapping raw AudioSpecificConfig.
func TestBuildAACSequenceHeaderFromConfig(t *testing.T) {
	t.Run("wraps config in RTMP tag format", func(t *testing.T) {
		// 2-byte AudioSpecificConfig for 44.1kHz stereo AAC-LC
		asc := []byte{0x12, 0x10}

		result := BuildAACSequenceHeaderFromConfig(asc)

		if len(result) != 4 { // 2 header + 2 config
			t.Fatalf("length = %d, want 4", len(result))
		}
		if result[0] != 0xAF {
			t.Errorf("byte 0 = 0x%02X, want 0xAF (AAC format byte)", result[0])
		}
		if result[1] != 0x00 {
			t.Errorf("byte 1 = 0x%02X, want 0x00 (sequence header)", result[1])
		}
		if !bytes.Equal(result[2:], asc) {
			t.Errorf("config data = %x, want %x", result[2:], asc)
		}
	})

	t.Run("empty config produces minimal header", func(t *testing.T) {
		result := BuildAACSequenceHeaderFromConfig(nil)
		if len(result) != 2 {
			t.Fatalf("length = %d, want 2", len(result))
		}
		if result[0] != 0xAF || result[1] != 0x00 {
			t.Errorf("header = [0x%02X, 0x%02X], want [0xAF, 0x00]", result[0], result[1])
		}
	})
}
