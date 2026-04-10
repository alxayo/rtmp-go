package ts

// This file implements the high-level MPEG-TS demuxer that ties all the
// lower-level components together. It:
//
//  1. Finds 188-byte packet boundaries in raw data (handles partial packets)
//  2. Parses each TS packet header to identify its PID
//  3. Reads the PAT (PID 0) to discover the PMT PID
//  4. Reads the PMT to discover audio/video elementary stream PIDs
//  5. Reassembles PES packets on each elementary stream PID
//  6. Delivers complete media frames to a callback function
//
// Usage:
//
//	demuxer := ts.NewDemuxer(func(frame *ts.MediaFrame) {
//	    fmt.Printf("Got %s frame, PTS=%d, %d bytes\n",
//	        frame.Stream.Codec, frame.PTS, len(frame.Data))
//	})
//	demuxer.Feed(tsData)  // Feed raw TS data (any amount)

import (
	"fmt"
	"log/slog"
)

// ElementaryStream represents a detected audio or video stream in the
// transport stream. Once the demuxer parses the PMT, it creates one of
// these for each stream it finds.
type ElementaryStream struct {
	// PID is the 13-bit transport stream Packet Identifier assigned to
	// this stream. All TS packets with this PID carry data for this stream.
	PID uint16

	// StreamType is the codec identifier from the PMT (see StreamType*
	// constants in stream_types.go).
	StreamType uint8

	// Codec is a human-readable name for the codec (e.g., "H.264", "AAC").
	Codec string
}

// MediaFrame is a single media access unit (video frame or audio frame)
// extracted from the transport stream, ready for further processing.
type MediaFrame struct {
	// Stream points to the ElementaryStream this frame belongs to,
	// so you can tell whether it's audio or video and which codec it uses.
	Stream *ElementaryStream

	// PTS is the Presentation Timestamp in 90kHz clock units.
	// Tells the player when to display this frame.
	// -1 if no PTS was present in the PES header.
	PTS int64

	// DTS is the Decode Timestamp in 90kHz clock units.
	// Tells the decoder when to decode this frame.
	// -1 if no DTS was present.
	DTS int64

	// Data contains the raw elementary stream bytes. For H.264, this is
	// one or more NAL units. For AAC, this is ADTS-framed audio data.
	Data []byte

	// IsKey is true when this frame is a keyframe (Random Access Point).
	// For video, this means an I-frame that can be decoded independently.
	// Detected via the random_access_indicator in the TS adaptation field.
	IsKey bool
}

// FrameHandler is a callback function that the demuxer calls for each
// complete media frame it extracts. Implement this to process the frames
// (e.g., re-mux into RTMP, write to disk, etc.).
type FrameHandler func(frame *MediaFrame)

// Demuxer processes MPEG-TS data and emits elementary stream frames.
//
// It maintains state across calls to Feed(), so you can feed it data
// in chunks of any size — it handles packet boundary alignment internally.
type Demuxer struct {
	// patParsed is true once we've successfully parsed the PAT and know
	// which PID carries the PMT.
	patParsed bool

	// pmtPID is the PID where the PMT for the first program is found.
	// Learned from parsing the PAT.
	pmtPID uint16

	// pmtParsed is true once we've successfully parsed the PMT and know
	// which PIDs carry audio and video data.
	pmtParsed bool

	// streams maps PID → ElementaryStream for each stream we discovered
	// in the PMT. We only create PES assemblers for these PIDs.
	streams map[uint16]*ElementaryStream

	// assemblers maps PID → PESAssembler for each elementary stream.
	// Each assembler collects TS payloads and reassembles them into
	// complete PES packets.
	assemblers map[uint16]*PESAssembler

	// ccCounters tracks the expected continuity counter for each PID.
	// If a packet's CC doesn't match what we expect, we know packets
	// were lost. Maps PID → expected next CC value.
	ccCounters map[uint16]uint8

	// ccInitialized tracks whether we've seen the first packet for each PID.
	// We can't check continuity until we've seen at least one packet.
	ccInitialized map[uint16]bool

	// handler is the callback function invoked for each complete media frame.
	handler FrameHandler

	// remainder holds leftover bytes from the previous Feed() call that
	// didn't form a complete 188-byte packet. These bytes will be
	// prepended to the next Feed() call's data.
	remainder []byte

	// randomAccess tracks whether the current PES packet started at a
	// random access point (keyframe), per PID.
	randomAccess map[uint16]bool
}

// NewDemuxer creates a new MPEG-TS demuxer that calls handler for each
// complete media frame extracted from the stream.
func NewDemuxer(handler FrameHandler) *Demuxer {
	return &Demuxer{
		streams:       make(map[uint16]*ElementaryStream),
		assemblers:    make(map[uint16]*PESAssembler),
		ccCounters:    make(map[uint16]uint8),
		ccInitialized: make(map[uint16]bool),
		handler:       handler,
		randomAccess:  make(map[uint16]bool),
	}
}

// Feed processes raw data that may contain multiple TS packets.
//
// The data doesn't need to be aligned to packet boundaries — Feed()
// handles sync byte detection and carries over partial packets between
// calls. This makes it safe to call with arbitrary chunk sizes from
// network reads.
func (d *Demuxer) Feed(data []byte) error {
	// Prepend any leftover bytes from the previous call.
	if len(d.remainder) > 0 {
		data = append(d.remainder, data...)
		d.remainder = nil
	}

	// Find the first sync byte to align to packet boundaries.
	// In a well-formed stream, the first byte should be 0x47, but after
	// errors or partial reads we may need to scan for it.
	startOffset := findSyncByte(data)
	if startOffset < 0 {
		// No sync byte found in the entire buffer — discard everything.
		return nil
	}

	// Process as many complete 188-byte packets as we can.
	pos := startOffset
	for pos+PacketSize <= len(data) {
		// Verify sync byte at the expected position. If it's not there,
		// we've lost sync — scan forward to find the next one.
		if data[pos] != SyncByte {
			nextSync := findSyncByte(data[pos:])
			if nextSync < 0 {
				break
			}
			pos += nextSync
			continue
		}

		// Parse the 188-byte packet.
		var pktData [PacketSize]byte
		copy(pktData[:], data[pos:pos+PacketSize])

		pkt, err := ParsePacket(pktData)
		if err != nil {
			// Skip this packet and try the next one.
			pos += PacketSize
			continue
		}

		// Process the parsed packet.
		d.processPacket(pkt)

		pos += PacketSize
	}

	// Save any remaining bytes that don't form a complete packet.
	// They'll be prepended to the next Feed() call.
	if pos < len(data) {
		d.remainder = make([]byte, len(data)-pos)
		copy(d.remainder, data[pos:])
	}

	return nil
}

// Streams returns the list of elementary streams discovered so far.
// This is populated after the PMT has been parsed.
func (d *Demuxer) Streams() []*ElementaryStream {
	result := make([]*ElementaryStream, 0, len(d.streams))
	for _, s := range d.streams {
		result = append(result, s)
	}
	return result
}

// processPacket handles a single parsed TS packet, dispatching it to the
// appropriate handler based on its PID.
func (d *Demuxer) processPacket(pkt *Packet) {
	// Skip null packets — they're just padding.
	if pkt.PID == NullPID {
		return
	}

	// Skip packets with transport errors.
	if pkt.TEI {
		return
	}

	// Check continuity counter for packet loss detection.
	d.checkContinuity(pkt)

	// Track random access points from the adaptation field.
	if pkt.HasAdaptation && pkt.AdaptationField != nil && pkt.AdaptationField.RandomAccess {
		d.randomAccess[pkt.PID] = true
	}

	// No payload? Nothing more to do.
	if !pkt.HasPayload || len(pkt.Payload) == 0 {
		return
	}

	// Dispatch based on PID and current parsing state.
	switch {
	case pkt.PID == PATPID:
		// PID 0 always carries the Program Association Table.
		d.handlePAT(pkt)

	case d.patParsed && pkt.PID == d.pmtPID:
		// This PID carries the PMT for our program.
		d.handlePMT(pkt)

	case d.pmtParsed:
		// Once we know the stream PIDs, check if this is a media PID.
		if _, ok := d.streams[pkt.PID]; ok {
			d.handleMedia(pkt)
		}
	}
}

// checkContinuity verifies the continuity counter for this PID.
// The CC is a 4-bit counter (0-15) that should increment by 1 for each
// packet with payload. If it jumps, packets were lost.
func (d *Demuxer) checkContinuity(pkt *Packet) {
	if !pkt.HasPayload {
		// Packets without payload don't increment the counter.
		return
	}

	if !d.ccInitialized[pkt.PID] {
		// First packet we've seen for this PID — just record the CC.
		d.ccCounters[pkt.PID] = (pkt.ContinuityCounter + 1) & 0x0F
		d.ccInitialized[pkt.PID] = true
		return
	}

	expected := d.ccCounters[pkt.PID]
	if pkt.ContinuityCounter != expected {
		// Discontinuity in the adaptation field resets the counter.
		if pkt.HasAdaptation && pkt.AdaptationField != nil && pkt.AdaptationField.Discontinuity {
			// Expected discontinuity — don't report an error.
		} else {
			slog.Debug("ts: continuity counter error",
				"pid", pkt.PID,
				"expected", expected,
				"got", pkt.ContinuityCounter,
			)
		}
	}

	// The next expected CC is current + 1, wrapping at 16.
	d.ccCounters[pkt.PID] = (pkt.ContinuityCounter + 1) & 0x0F
}

// handlePAT processes a PAT packet to discover the PMT PID.
func (d *Demuxer) handlePAT(pkt *Packet) {
	entries, err := ParsePAT(pkt.Payload)
	if err != nil {
		slog.Debug("ts: failed to parse PAT", "error", err)
		return
	}

	// Find the first real program (program number > 0).
	// Program 0 is reserved for the Network Information Table.
	for _, entry := range entries {
		if entry.ProgramNumber > 0 {
			d.pmtPID = entry.PMTPID
			d.patParsed = true
			return
		}
	}
}

// handlePMT processes a PMT packet to discover elementary stream PIDs.
func (d *Demuxer) handlePMT(pkt *Packet) {
	pmt, err := ParsePMT(pkt.Payload)
	if err != nil {
		slog.Debug("ts: failed to parse PMT", "error", err)
		return
	}

	// Register each elementary stream we care about.
	for _, stream := range pmt.Streams {
		codec := StreamTypeName(stream.StreamType)

		// Only track streams with known codec types.
		if codec == "Unknown" {
			continue
		}

		// Create the ElementaryStream and its PES assembler if we haven't already.
		if _, exists := d.streams[stream.PID]; !exists {
			d.streams[stream.PID] = &ElementaryStream{
				PID:        stream.PID,
				StreamType: stream.StreamType,
				Codec:      codec,
			}
			d.assemblers[stream.PID] = &PESAssembler{}
		}
	}

	d.pmtParsed = true
}

// handleMedia feeds a media packet's payload into the appropriate PES
// assembler and emits a MediaFrame when a PES packet completes.
func (d *Demuxer) handleMedia(pkt *Packet) {
	assembler, ok := d.assemblers[pkt.PID]
	if !ok {
		return
	}

	// Feed the payload to the PES assembler. If a previous PES packet
	// is now complete (because a new one is starting), we get it back.
	pesPkt := assembler.Feed(pkt.Payload, pkt.PayloadUnitStart)
	if pesPkt != nil {
		d.emitFrame(pkt.PID, pesPkt)
	}
}

// emitFrame creates a MediaFrame from a completed PES packet and calls
// the handler callback.
func (d *Demuxer) emitFrame(pid uint16, pesPkt *PESPacket) {
	stream, ok := d.streams[pid]
	if !ok || d.handler == nil {
		return
	}

	// Skip empty frames.
	if len(pesPkt.Data) == 0 {
		return
	}

	frame := &MediaFrame{
		Stream: stream,
		PTS:    pesPkt.PTS,
		DTS:    pesPkt.DTS,
		Data:   pesPkt.Data,
		IsKey:  d.randomAccess[pid],
	}

	// Clear the random access flag — it only applies to the first PES
	// packet after the flag was set.
	delete(d.randomAccess, pid)

	d.handler(frame)
}

// Flush forces all PES assemblers to emit their buffered data.
// Call this at end-of-stream to get any final frames.
func (d *Demuxer) Flush() {
	for pid, assembler := range d.assemblers {
		if pesPkt := assembler.Flush(); pesPkt != nil {
			d.emitFrame(pid, pesPkt)
		}
	}
}

// findSyncByte scans data for the MPEG-TS sync byte (0x47).
// Returns the offset of the first sync byte, or -1 if not found.
//
// For robustness, we could verify that another 0x47 appears exactly
// 188 bytes later, but for our use case (SRT with MPEG-TS), the input
// is already well-formed, so a single sync byte check suffices.
func findSyncByte(data []byte) int {
	for i, b := range data {
		if b == SyncByte {
			return i
		}
	}
	return -1
}

// String returns a human-readable description of the demuxer state,
// useful for debugging.
func (d *Demuxer) String() string {
	if !d.patParsed {
		return "Demuxer{waiting for PAT}"
	}
	if !d.pmtParsed {
		return fmt.Sprintf("Demuxer{PMT PID=%d, waiting for PMT}", d.pmtPID)
	}
	return fmt.Sprintf("Demuxer{%d streams}", len(d.streams))
}
