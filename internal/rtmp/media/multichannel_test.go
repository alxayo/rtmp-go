package media

import "testing"

// TestParseMultichannelConfig_Stereo verifies parsing a stereo (2-channel) config
// with unspecified channel order. This is the most common case for standard stereo
// audio where channel positions don't need explicit mapping.
func TestParseMultichannelConfig_Stereo(t *testing.T) {
	// byte 0: ChannelOrder=0 (Unspecified) in high nibble, ChannelCount=2 in low nibble
	// Binary: 0000_0010 = 0x02
	data := []byte{0x02}
	cfg, err := ParseMultichannelConfig(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if cfg.ChannelOrder != ChannelOrderUnspecified {
		_tFatalf(t, "channel order mismatch: got %d, want %d", cfg.ChannelOrder, ChannelOrderUnspecified)
	}
	if cfg.ChannelCount != 2 {
		_tFatalf(t, "channel count mismatch: got %d, want 2", cfg.ChannelCount)
	}
	// Unspecified order should have no per-channel mapping.
	if len(cfg.ChannelMapping) != 0 {
		_tFatalf(t, "expected empty channel mapping for unspecified order, got %v", cfg.ChannelMapping)
	}
}

// TestParseMultichannelConfig_Surround51 verifies parsing a 5.1 surround (6-channel)
// config with native channel order. Native order means the player uses the codec's
// built-in channel layout (e.g., AAC: center, front-left, front-right, surround-left,
// surround-right, LFE).
func TestParseMultichannelConfig_Surround51(t *testing.T) {
	// byte 0: ChannelOrder=1 (Native) in high nibble, ChannelCount=6 in low nibble
	// Binary: 0001_0110 = 0x16
	data := []byte{0x16}
	cfg, err := ParseMultichannelConfig(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if cfg.ChannelOrder != ChannelOrderNative {
		_tFatalf(t, "channel order mismatch: got %d, want %d", cfg.ChannelOrder, ChannelOrderNative)
	}
	if cfg.ChannelCount != 6 {
		_tFatalf(t, "channel count mismatch: got %d, want 6", cfg.ChannelCount)
	}
	// Native order should have no per-channel mapping.
	if len(cfg.ChannelMapping) != 0 {
		_tFatalf(t, "expected empty channel mapping for native order, got %v", cfg.ChannelMapping)
	}
}

// TestParseMultichannelConfig_CustomMapping verifies parsing a custom multichannel
// config where each channel has an explicit speaker assignment. This is used for
// non-standard layouts (e.g., Atmos bed channels, binaural, custom studio setups).
func TestParseMultichannelConfig_CustomMapping(t *testing.T) {
	// byte 0: ChannelOrder=2 (Custom) in high nibble, ChannelCount=4 in low nibble
	// Binary: 0010_0100 = 0x24
	// bytes 1-4: per-channel speaker positions (arbitrary test values)
	//   0x01 = front-left, 0x02 = front-right, 0x03 = center, 0x04 = LFE (hypothetical)
	data := []byte{0x24, 0x01, 0x02, 0x03, 0x04}
	cfg, err := ParseMultichannelConfig(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if cfg.ChannelOrder != ChannelOrderCustom {
		_tFatalf(t, "channel order mismatch: got %d, want %d", cfg.ChannelOrder, ChannelOrderCustom)
	}
	if cfg.ChannelCount != 4 {
		_tFatalf(t, "channel count mismatch: got %d, want 4", cfg.ChannelCount)
	}
	// Custom order should contain the per-channel speaker assignments.
	if len(cfg.ChannelMapping) != 4 {
		_tFatalf(t, "channel mapping length mismatch: got %d, want 4", len(cfg.ChannelMapping))
	}
	expectedMapping := []uint8{0x01, 0x02, 0x03, 0x04}
	for i, v := range cfg.ChannelMapping {
		if v != expectedMapping[i] {
			_tFatalf(t, "channel mapping[%d] mismatch: got 0x%02x, want 0x%02x", i, v, expectedMapping[i])
		}
	}
}

// TestParseMultichannelConfig_CustomPartialMapping verifies that custom order
// with fewer mapping bytes than the channel count still parses without error.
// This handles the case where the sender provides incomplete mapping data.
func TestParseMultichannelConfig_CustomPartialMapping(t *testing.T) {
	// ChannelOrder=2 (Custom), ChannelCount=4, but only 2 mapping bytes follow
	data := []byte{0x24, 0x01, 0x02}
	cfg, err := ParseMultichannelConfig(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if cfg.ChannelCount != 4 {
		_tFatalf(t, "channel count mismatch: got %d, want 4", cfg.ChannelCount)
	}
	// Should only have 2 mapping entries (what was available in the data).
	if len(cfg.ChannelMapping) != 2 {
		_tFatalf(t, "channel mapping length mismatch: got %d, want 2", len(cfg.ChannelMapping))
	}
}

// TestParseMultichannelConfig_TooShort verifies that an empty payload is rejected
// (we need at least 1 byte for the channel order + count header).
func TestParseMultichannelConfig_TooShort(t *testing.T) {
	_, err := ParseMultichannelConfig([]byte{})
	if err == nil {
		t.Fatal("expected error for empty multichannel config data")
	}
}

// TestParseMultichannelConfig_Mono verifies parsing a mono (1-channel) config
// with unspecified order, the simplest possible multichannel config.
func TestParseMultichannelConfig_Mono(t *testing.T) {
	// ChannelOrder=0, ChannelCount=1 → 0x01
	data := []byte{0x01}
	cfg, err := ParseMultichannelConfig(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if cfg.ChannelCount != 1 {
		_tFatalf(t, "channel count mismatch: got %d, want 1", cfg.ChannelCount)
	}
}

// TestParseMultichannelConfig_71Surround verifies parsing a 7.1 surround (8-channel)
// config with native order.
func TestParseMultichannelConfig_71Surround(t *testing.T) {
	// ChannelOrder=1 (Native), ChannelCount=8 → 0x18
	data := []byte{0x18}
	cfg, err := ParseMultichannelConfig(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if cfg.ChannelOrder != ChannelOrderNative {
		_tFatalf(t, "channel order mismatch: got %d, want %d", cfg.ChannelOrder, ChannelOrderNative)
	}
	if cfg.ChannelCount != 8 {
		_tFatalf(t, "channel count mismatch: got %d, want 8", cfg.ChannelCount)
	}
}
