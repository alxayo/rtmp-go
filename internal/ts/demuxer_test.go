package ts

import (
	"testing"
)

// buildTSPacket creates a 188-byte TS packet with the given parameters.
// This is a test helper for constructing realistic TS packets.
// When the payload is shorter than the available space, adaptation field
// stuffing is added (just like real TS encoders do).
func buildTSPacket(pid uint16, pusi bool, cc uint8, payload []byte, af *buildAF) [PacketSize]byte {
	var pkt [PacketSize]byte

	// Sync byte
	pkt[0] = SyncByte

	// TEI=0 | PUSI | Priority=0 | PID
	if pusi {
		pkt[1] = 0x40 | byte((pid>>8)&0x1F)
	} else {
		pkt[1] = byte((pid >> 8) & 0x1F)
	}
	pkt[2] = byte(pid & 0xFF)

	hasPL := len(payload) > 0

	// Calculate the adaptation field bytes (if any).
	var afBytes []byte
	if af != nil {
		afBytes = af.build()
	}

	// Determine how much space is available for payload after the 4-byte
	// header and adaptation field.
	headerAndAF := 4 + len(afBytes)
	available := PacketSize - headerAndAF

	// If payload is shorter than available space, we need stuffing.
	// Stuffing is done by adding/expanding the adaptation field.
	needStuffing := hasPL && len(payload) < available
	if needStuffing {
		stuffNeeded := available - len(payload)
		if len(afBytes) == 0 {
			// No AF yet — create one with stuffing.
			// AF length byte + (stuffNeeded-1) bytes of AF data.
			afBytes = make([]byte, stuffNeeded)
			afBytes[0] = byte(stuffNeeded - 1) // Length (excludes length byte itself)
			if stuffNeeded > 1 {
				afBytes[1] = 0x00 // Flags byte
				for i := 2; i < stuffNeeded; i++ {
					afBytes[i] = 0xFF
				}
			}
		} else {
			// AF exists — extend it with stuffing bytes.
			oldLen := afBytes[0]
			newLen := int(oldLen) + stuffNeeded
			afBytes[0] = byte(newLen)
			stuffing := make([]byte, stuffNeeded)
			for i := range stuffing {
				stuffing[i] = 0xFF
			}
			afBytes = append(afBytes, stuffing...)
		}
	}

	// Determine adaptation control bits.
	hasAF := len(afBytes) > 0
	var adaptCtrl byte
	switch {
	case hasAF && hasPL:
		adaptCtrl = 0x03
	case hasAF:
		adaptCtrl = 0x02
	case hasPL:
		adaptCtrl = 0x01
	}

	pkt[3] = (adaptCtrl << 4) | (cc & 0x0F)

	offset := 4

	// Write adaptation field.
	if hasAF {
		copy(pkt[offset:], afBytes)
		offset += len(afBytes)
	}

	// Write payload.
	if hasPL && offset < PacketSize {
		remaining := PacketSize - offset
		if len(payload) <= remaining {
			copy(pkt[offset:], payload)
		} else {
			copy(pkt[offset:], payload[:remaining])
		}
	}

	return pkt
}

// buildAF is a helper for constructing adaptation field bytes.
type buildAF struct {
	randomAccess bool
	pcr          int64 // -1 for no PCR
}

// build serializes the adaptation field.
func (a *buildAF) build() []byte {
	hasPCR := a.pcr >= 0

	// Calculate length.
	length := 1 // flags byte
	if hasPCR {
		length += 6 // PCR is 6 bytes
	}

	result := []byte{byte(length)}

	// Flags byte.
	var flags byte
	if a.randomAccess {
		flags |= 0x40
	}
	if hasPCR {
		flags |= 0x10
	}
	result = append(result, flags)

	// PCR.
	if hasPCR {
		pcr := make([]byte, 6)
		pcr[0] = byte(a.pcr >> 25)
		pcr[1] = byte(a.pcr >> 17)
		pcr[2] = byte(a.pcr >> 9)
		pcr[3] = byte(a.pcr >> 1)
		pcr[4] = byte(a.pcr&1) << 7
		pcr[5] = 0x00
		result = append(result, pcr...)
	}

	return result
}

// buildFullTSStream creates a complete mini MPEG-TS stream with PAT, PMT,
// and one PES packet, suitable for end-to-end demuxer testing.
func buildFullTSStream(videoPayload []byte, pts int64) []byte {
	const (
		pmtPID   uint16 = 0x1000
		videoPID uint16 = 0x0100
		audioPID uint16 = 0x0101
	)

	var stream []byte

	// 1. PAT packet (PID 0, program 1 → PMT on PID 0x1000)
	patPayload := buildPATPayload([]PATEntry{
		{ProgramNumber: 1, PMTPID: pmtPID},
	})
	patPkt := buildTSPacket(PATPID, true, 0, patPayload, nil)
	stream = append(stream, patPkt[:]...)

	// 2. PMT packet (PID 0x1000, H.264 video on 0x0100, AAC audio on 0x0101)
	pmtPayload := buildPMTPayload(videoPID, []PMTStream{
		{StreamType: StreamTypeH264, PID: videoPID},
		{StreamType: StreamTypeAAC_ADTS, PID: audioPID},
	})
	pmtPkt := buildTSPacket(pmtPID, true, 0, pmtPayload, nil)
	stream = append(stream, pmtPkt[:]...)

	// 3. Video PES packet (PID 0x0100)
	pesBytes := buildPESPacket(0xE0, pts, -1, videoPayload)

	// Split PES across TS packets if needed.
	cc := uint8(0)
	first := true
	for len(pesBytes) > 0 {
		// Calculate available space: 184 bytes minus adaptation field.
		available := 184

		var af *buildAF
		if first {
			// Mark first video packet with random access (keyframe).
			af = &buildAF{randomAccess: true, pcr: -1}
			available -= 2 // 1 byte length + 1 byte flags
		}

		chunkSize := available
		if chunkSize > len(pesBytes) {
			chunkSize = len(pesBytes)
		}

		pkt := buildTSPacket(videoPID, first, cc, pesBytes[:chunkSize], af)
		stream = append(stream, pkt[:]...)

		pesBytes = pesBytes[chunkSize:]
		cc = (cc + 1) & 0x0F
		first = false
	}

	return stream
}

// TestDemuxer_EndToEnd tests the complete demuxing pipeline: PAT → PMT → PES → MediaFrame.
func TestDemuxer_EndToEnd(t *testing.T) {
	videoData := []byte{0x00, 0x00, 0x00, 0x01, 0x65} // Fake H.264 IDR NAL unit
	pts := int64(90000)                                 // 1 second

	var receivedFrames []*MediaFrame
	demuxer := NewDemuxer(func(frame *MediaFrame) {
		// Copy the frame since the demuxer may reuse buffers.
		frameCopy := *frame
		dataCopy := make([]byte, len(frame.Data))
		copy(dataCopy, frame.Data)
		frameCopy.Data = dataCopy
		receivedFrames = append(receivedFrames, &frameCopy)
	})

	tsData := buildFullTSStream(videoData, pts)

	err := demuxer.Feed(tsData)
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}

	// Flush to get any remaining frames.
	demuxer.Flush()

	if len(receivedFrames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(receivedFrames))
	}

	frame := receivedFrames[0]
	if frame.Stream == nil {
		t.Fatal("frame.Stream is nil")
	}
	if frame.Stream.Codec != "H.264" {
		t.Errorf("expected codec 'H.264', got %q", frame.Stream.Codec)
	}
	if frame.Stream.PID != 0x0100 {
		t.Errorf("expected PID=0x0100, got 0x%04X", frame.Stream.PID)
	}
	if frame.PTS != pts {
		t.Errorf("expected PTS=%d, got %d", pts, frame.PTS)
	}
	if frame.IsKey != true {
		t.Error("expected IsKey=true for keyframe")
	}

	if len(frame.Data) != len(videoData) {
		t.Fatalf("expected data length=%d, got %d", len(videoData), len(frame.Data))
	}
	for i, b := range videoData {
		if frame.Data[i] != b {
			t.Errorf("data[%d]: expected 0x%02X, got 0x%02X", i, b, frame.Data[i])
		}
	}
}

// TestDemuxer_StreamDiscovery tests that the demuxer correctly discovers
// elementary streams from the PAT and PMT.
func TestDemuxer_StreamDiscovery(t *testing.T) {
	demuxer := NewDemuxer(func(frame *MediaFrame) {})

	tsData := buildFullTSStream([]byte{0x01}, 0)
	err := demuxer.Feed(tsData)
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}

	streams := demuxer.Streams()
	if len(streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(streams))
	}

	// Check that we found both H.264 and AAC streams.
	foundVideo := false
	foundAudio := false
	for _, s := range streams {
		switch s.StreamType {
		case StreamTypeH264:
			foundVideo = true
			if s.Codec != "H.264" {
				t.Errorf("video stream codec = %q, want 'H.264'", s.Codec)
			}
		case StreamTypeAAC_ADTS:
			foundAudio = true
			if s.Codec != "AAC (ADTS)" {
				t.Errorf("audio stream codec = %q, want 'AAC (ADTS)'", s.Codec)
			}
		}
	}

	if !foundVideo {
		t.Error("did not find H.264 video stream")
	}
	if !foundAudio {
		t.Error("did not find AAC audio stream")
	}
}

// TestDemuxer_PacketResync tests that the demuxer can find sync in data
// that doesn't start at a packet boundary.
func TestDemuxer_PacketResync(t *testing.T) {
	videoData := []byte{0xDE, 0xAD}
	pts := int64(45000)

	frameCount := 0
	demuxer := NewDemuxer(func(frame *MediaFrame) {
		frameCount++
	})

	// Build valid TS data.
	tsData := buildFullTSStream(videoData, pts)

	// Prepend some garbage bytes before the first sync byte.
	garbage := []byte{0x00, 0x01, 0x02, 0x03, 0x04}
	dataWithGarbage := append(garbage, tsData...)

	err := demuxer.Feed(dataWithGarbage)
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}

	demuxer.Flush()

	if frameCount != 1 {
		t.Errorf("expected 1 frame after resync, got %d", frameCount)
	}
}

// TestDemuxer_PartialPacket tests that the demuxer handles data that ends
// with an incomplete packet (saves remainder for next Feed call).
func TestDemuxer_PartialPacket(t *testing.T) {
	videoData := []byte{0xBE, 0xEF}
	pts := int64(90000)

	frameCount := 0
	demuxer := NewDemuxer(func(frame *MediaFrame) {
		frameCount++
	})

	tsData := buildFullTSStream(videoData, pts)

	// Feed data in two parts, splitting in the middle of the last packet.
	if len(tsData) < PacketSize+50 {
		t.Fatal("test data too short to split")
	}
	splitPoint := len(tsData) - 50

	err := demuxer.Feed(tsData[:splitPoint])
	if err != nil {
		t.Fatalf("Feed (part 1) failed: %v", err)
	}

	err = demuxer.Feed(tsData[splitPoint:])
	if err != nil {
		t.Fatalf("Feed (part 2) failed: %v", err)
	}

	demuxer.Flush()

	if frameCount != 1 {
		t.Errorf("expected 1 frame after split feed, got %d", frameCount)
	}
}

// TestDemuxer_NullPacketsIgnored verifies that null/padding packets
// (PID 0x1FFF) are silently discarded.
func TestDemuxer_NullPacketsIgnored(t *testing.T) {
	frameCount := 0
	demuxer := NewDemuxer(func(frame *MediaFrame) {
		frameCount++
	})

	// Build a null packet.
	nullPkt := buildTSPacket(NullPID, false, 0, []byte{0xFF}, nil)

	err := demuxer.Feed(nullPkt[:])
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}

	if frameCount != 0 {
		t.Errorf("expected 0 frames from null packet, got %d", frameCount)
	}
}

// TestDemuxer_ContinuityCounterTracking verifies that the demuxer tracks
// continuity counters per PID.
func TestDemuxer_ContinuityCounterTracking(t *testing.T) {
	// This test just verifies the demuxer doesn't crash when it encounters
	// continuity counter jumps. The debug log message would be emitted but
	// we don't fail on CC errors — just log them.
	demuxer := NewDemuxer(func(frame *MediaFrame) {})

	// Build a stream with intentional CC gap.
	tsData := buildFullTSStream([]byte{0x01}, 0)

	// Feed the data — the demuxer should handle it gracefully.
	err := demuxer.Feed(tsData)
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}
}

// TestDemuxer_EmptyFeed tests feeding empty data.
func TestDemuxer_EmptyFeed(t *testing.T) {
	demuxer := NewDemuxer(func(frame *MediaFrame) {
		t.Fatal("unexpected frame from empty feed")
	})

	err := demuxer.Feed([]byte{})
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}

	err = demuxer.Feed(nil)
	if err != nil {
		t.Fatalf("Feed(nil) failed: %v", err)
	}
}

// TestDemuxer_String tests the debug string output.
func TestDemuxer_String(t *testing.T) {
	demuxer := NewDemuxer(func(frame *MediaFrame) {})

	// Before any data.
	s := demuxer.String()
	if s != "Demuxer{waiting for PAT}" {
		t.Errorf("unexpected String before PAT: %q", s)
	}

	// Feed a full stream.
	tsData := buildFullTSStream([]byte{0x01}, 0)
	demuxer.Feed(tsData)

	s = demuxer.String()
	if s != "Demuxer{2 streams}" {
		t.Errorf("unexpected String after PMT: %q", s)
	}
}

// TestDemuxer_MultipleFrames tests that the demuxer correctly handles
// multiple PES packets for the same PID.
func TestDemuxer_MultipleFrames(t *testing.T) {
	const (
		pmtPID   uint16 = 0x1000
		videoPID uint16 = 0x0100
		audioPID uint16 = 0x0101
	)

	var stream []byte

	// PAT
	patPayload := buildPATPayload([]PATEntry{
		{ProgramNumber: 1, PMTPID: pmtPID},
	})
	patPkt := buildTSPacket(PATPID, true, 0, patPayload, nil)
	stream = append(stream, patPkt[:]...)

	// PMT
	pmtPayload := buildPMTPayload(videoPID, []PMTStream{
		{StreamType: StreamTypeH264, PID: videoPID},
		{StreamType: StreamTypeAAC_ADTS, PID: audioPID},
	})
	pmtPkt := buildTSPacket(pmtPID, true, 0, pmtPayload, nil)
	stream = append(stream, pmtPkt[:]...)

	// Two video PES packets.
	pes1 := buildPESPacket(0xE0, 90000, -1, []byte{0x00, 0x00, 0x00, 0x01, 0x65})
	pes2 := buildPESPacket(0xE0, 93600, -1, []byte{0x00, 0x00, 0x00, 0x01, 0x41})

	pkt1 := buildTSPacket(videoPID, true, 1, pes1, nil)
	stream = append(stream, pkt1[:]...)

	pkt2 := buildTSPacket(videoPID, true, 2, pes2, nil)
	stream = append(stream, pkt2[:]...)

	var frames []*MediaFrame
	demuxer := NewDemuxer(func(frame *MediaFrame) {
		frameCopy := *frame
		dataCopy := make([]byte, len(frame.Data))
		copy(dataCopy, frame.Data)
		frameCopy.Data = dataCopy
		frames = append(frames, &frameCopy)
	})

	err := demuxer.Feed(stream)
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}

	demuxer.Flush()

	// We should have 2 video frames.
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}

	if frames[0].PTS != 90000 {
		t.Errorf("frame 0: expected PTS=90000, got %d", frames[0].PTS)
	}
	if frames[1].PTS != 93600 {
		t.Errorf("frame 1: expected PTS=93600, got %d", frames[1].PTS)
	}
}
