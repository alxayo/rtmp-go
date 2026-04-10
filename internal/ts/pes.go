package ts

// This file handles PES (Packetized Elementary Stream) reassembly.
//
// In MPEG-TS, raw audio/video data (elementary streams) are first wrapped in
// PES packets, then those PES packets are split across one or more 188-byte
// TS packets. Think of it like putting a letter (the video frame) into an
// envelope (PES packet), then cutting the envelope into pieces that each fit
// into a fixed-size mailbox slot (TS packet).
//
// The PES header contains important timing information:
//   - PTS (Presentation Timestamp): When this frame should be displayed
//   - DTS (Decode Timestamp): When this frame should be decoded
//
// For audio and for B-frame-free video, PTS == DTS. B-frames in H.264 cause
// DTS to differ from PTS because frames must be decoded in a different order
// than they're displayed.

import (
	"bytes"
)

// PESPacket represents a fully reassembled PES packet containing elementary
// stream data (e.g., one or more video/audio frames).
type PESPacket struct {
	// StreamID identifies the type of stream:
	//   0xE0-0xEF = video streams
	//   0xC0-0xDF = audio streams
	// This comes from the PES header, not the PMT.
	StreamID uint8

	// PTS is the Presentation Timestamp in 90kHz clock units.
	// Tells the player when to display this frame.
	// Set to -1 if no PTS is present in the PES header.
	PTS int64

	// DTS is the Decode Timestamp in 90kHz clock units.
	// Tells the decoder when to decode this frame (may differ from PTS
	// for video with B-frames). Set to -1 if no DTS is present.
	DTS int64

	// Data contains the raw elementary stream bytes (H.264 NALUs, AAC frames, etc.)
	// with PES headers stripped away.
	Data []byte
}

// PESAssembler collects TS packet payloads for a single PID and reassembles
// them into complete PES packets.
//
// How it works:
//  1. When a TS packet arrives with PayloadUnitStart=true, we know a new PES
//     packet is beginning. If we already had data buffered from a previous PES
//     packet, that previous packet is now complete — we parse and return it.
//  2. When PayloadUnitStart=false, this is a continuation of the current PES
//     packet — we just append the payload to our buffer.
//  3. When Flush() is called (e.g., at end-of-stream), we return whatever is
//     in the buffer as the final PES packet.
type PESAssembler struct {
	// buffer accumulates payload bytes across multiple TS packets
	// until a complete PES packet can be assembled.
	buffer bytes.Buffer

	// hasPESStart tracks whether we've seen the start of a PES packet.
	// Until we see a TS packet with PayloadUnitStart=true, we don't
	// know where a PES packet begins, so we discard data.
	hasPESStart bool

	// streamID remembers the stream ID from the last PES header we parsed.
	streamID uint8
}

// Feed processes one TS packet's payload for this PID.
//
// When payloadUnitStart is true, it means a new PES packet starts in this
// payload. If we had accumulated data from a previous PES packet, we parse
// and return that completed packet.
//
// Returns a complete PES packet if one was finished, nil otherwise.
func (a *PESAssembler) Feed(payload []byte, payloadUnitStart bool) *PESPacket {
	if len(payload) == 0 {
		return nil
	}

	var completed *PESPacket

	if payloadUnitStart {
		// A new PES packet is starting. If we had buffered data from the
		// previous PES packet, that packet is now complete — parse it.
		if a.hasPESStart && a.buffer.Len() > 0 {
			completed = parsePESPacket(a.buffer.Bytes())
		}

		// Reset the buffer for the new PES packet.
		a.buffer.Reset()
		a.hasPESStart = true
	}

	// Only accumulate data if we've seen the start of a PES packet.
	// Without a start marker, we don't know where the PES header is,
	// so any data would be unusable.
	if a.hasPESStart {
		a.buffer.Write(payload)
	}

	return completed
}

// Flush returns any PES packet that's currently being assembled.
// Call this when the stream ends to get the final PES packet.
func (a *PESAssembler) Flush() *PESPacket {
	if !a.hasPESStart || a.buffer.Len() == 0 {
		return nil
	}

	pkt := parsePESPacket(a.buffer.Bytes())
	a.buffer.Reset()
	a.hasPESStart = false
	return pkt
}

// parsePESPacket extracts the stream ID, PTS, DTS, and raw data from a
// reassembled PES packet's bytes.
//
// PES packet layout:
//
//	Bytes 0-2:  packet start code prefix (0x00, 0x00, 0x01)
//	Byte 3:     stream_id
//	Bytes 4-5:  PES packet length (can be 0 for video = unbounded)
//	Byte 6:     '10'(2) | scrambling(2) | priority(1) | alignment(1) | copyright(1) | original(1)
//	Byte 7:     PTS_DTS_flags(2) | ESCR_flag(1) | ES_rate_flag(1) | DSM_trick_mode(1) |
//	            additional_copy_info(1) | CRC_flag(1) | extension_flag(1)
//	Byte 8:     PES header data length (how many more header bytes follow)
//	Bytes 9+:   optional PTS, DTS, then payload data
func parsePESPacket(data []byte) *PESPacket {
	// A PES packet needs at least 9 bytes for the fixed header fields.
	if len(data) < 9 {
		return nil
	}

	// Verify the PES start code prefix (0x000001).
	if data[0] != 0x00 || data[1] != 0x00 || data[2] != 0x01 {
		return nil
	}

	pkt := &PESPacket{
		StreamID: data[3],
		PTS:      -1, // Default: no timestamp present
		DTS:      -1,
	}

	// PTS_DTS_flags are bits 7-6 of byte 7:
	//   00 = no PTS or DTS
	//   01 = forbidden
	//   10 = PTS only
	//   11 = both PTS and DTS
	ptsDtsFlags := (data[7] >> 6) & 0x03

	// PES header data length (byte 8) tells us how many bytes of optional
	// header fields follow before the actual stream data begins.
	pesHeaderDataLength := int(data[8])

	// The payload starts after the 9 fixed bytes plus the header data.
	payloadStart := 9 + pesHeaderDataLength
	if payloadStart > len(data) {
		// Truncated packet — return what we can.
		return nil
	}

	// Parse PTS if present (requires 5 bytes starting at byte 9).
	if ptsDtsFlags >= 0x02 && len(data) >= 14 {
		pkt.PTS = parseTimestamp(data[9:14])
	}

	// Parse DTS if present (requires 5 more bytes after the PTS).
	if ptsDtsFlags == 0x03 && len(data) >= 19 {
		pkt.DTS = parseTimestamp(data[14:19])
	}

	// If DTS is absent but PTS is present, DTS equals PTS.
	// This is the common case for audio and non-B-frame video.
	if pkt.PTS >= 0 && pkt.DTS < 0 {
		pkt.DTS = pkt.PTS
	}

	// The remaining bytes are the raw elementary stream data.
	pkt.Data = make([]byte, len(data)-payloadStart)
	copy(pkt.Data, data[payloadStart:])

	return pkt
}

// parseTimestamp extracts a 33-bit PTS or DTS value from its 5-byte encoding.
//
// The 33-bit timestamp is encoded with marker bits sprinkled in for sync:
//
//	Byte 0:    prefix(4 bits) | ts[32:30](3 bits) | marker_bit(1)
//	Byte 1-2:  ts[29:15](15 bits) | marker_bit(1)
//	Byte 3-4:  ts[14:0](15 bits) | marker_bit(1)
//
// The marker bits (always 1) help with error detection. We mask them out
// to reconstruct the original 33-bit value.
func parseTimestamp(data []byte) int64 {
	// Extract the three segments of the 33-bit timestamp.
	// Segment 1: bits 32-30 from byte 0 (3 bits, between prefix and marker)
	// Segment 2: bits 29-15 from bytes 1-2 (15 bits, before marker)
	// Segment 3: bits 14-0 from bytes 3-4 (15 bits, before marker)
	ts := int64(data[0]>>1&0x07) << 30
	ts |= int64(data[1]) << 22
	ts |= int64(data[2]>>1) << 15
	ts |= int64(data[3]) << 7
	ts |= int64(data[4] >> 1)

	return ts
}
