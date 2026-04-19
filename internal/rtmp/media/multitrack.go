package media

// Multitrack Support — E-RTMP v2
//
// Multitrack allows multiple audio or video tracks to be carried in a single
// RTMP stream. This is used for scenarios like:
//   - Multiple camera angles in one stream
//   - Multiple audio languages
//   - Commentary + game audio separation
//
// Wire format (after FourCC):
//
//	byte 0: [AvMultitrackType:4][PacketType:4]
//	For OneTrack/ManyTracks: byte 1 = trackId, then track data
//	For ManyTracksManyCodecs: byte 1 = trackId, bytes 2-5 = per-track FourCC
//
// The server recognizes multitrack packets and caches per-track sequence
// headers for late-join subscriber delivery. Multitrack messages are relayed
// to subscribers as-is (the raw RTMP message is not demultiplexed).

import "fmt"

// AvMultitrackType identifies the multitrack sub-type.
const (
	// MultitrackTypeOneTrack means the message contains a single track
	// with an explicit track ID (used when track ID != 0).
	MultitrackTypeOneTrack uint8 = 0

	// MultitrackTypeManyTracks means the message contains multiple tracks,
	// all using the same codec (identified by the FourCC in the outer header).
	MultitrackTypeManyTracks uint8 = 1

	// MultitrackTypeManyTracksManyCodecs means the message contains multiple
	// tracks with potentially different codecs (each track has its own FourCC).
	MultitrackTypeManyTracksManyCodecs uint8 = 2
)

// MultitrackMessage represents a parsed multitrack container.
type MultitrackMessage struct {
	// MultitrackType indicates the track layout (one, many, many+codecs).
	MultitrackType uint8

	// InnerPacketType is the actual packet type of the wrapped content
	// (e.g., SequenceStart=0, CodedFrames=1, etc.).
	InnerPacketType uint8

	// Tracks contains the parsed track data.
	Tracks []TrackData
}

// TrackData represents a single track within a multitrack message.
type TrackData struct {
	// TrackID identifies this track (0 = default/primary track).
	TrackID uint8

	// FourCC is the codec FourCC for this track (only set for ManyTracksManyCodecs).
	FourCC string

	// Data is the raw track payload (codec configuration or coded frames).
	Data []byte
}

// ParseMultitrack parses a multitrack video or audio packet.
// The input should be the payload AFTER the FourCC bytes (i.e., what's in
// VideoMessage.Payload or AudioMessage.Payload when PacketType is "multitrack").
//
// Note: This is a minimal implementation that extracts track metadata.
// Full per-track demultiplexing and independent processing is planned for
// a future release. Currently, multitrack messages are passed through to
// subscribers as-is (the raw RTMP message is relayed without modification).
func ParseMultitrack(data []byte) (*MultitrackMessage, error) {
	// Minimum 2 bytes: 1 byte header (type + inner pkt type) + 1 byte track ID.
	if len(data) < 2 {
		return nil, fmt.Errorf("multitrack.parse: payload too short (need >= 2 bytes, got %d)", len(data))
	}

	// First byte: high nibble = AvMultitrackType, low nibble = inner PacketType.
	multitrackType := (data[0] >> 4) & 0x0F
	innerPktType := data[0] & 0x0F

	msg := &MultitrackMessage{
		MultitrackType:  multitrackType,
		InnerPacketType: innerPktType,
	}

	// Parse based on multitrack type.
	switch multitrackType {
	case MultitrackTypeOneTrack:
		// Single track with explicit ID: [trackId(1B)][trackData...]
		track := TrackData{
			TrackID: data[1],
			Data:    data[2:],
		}
		msg.Tracks = []TrackData{track}

	case MultitrackTypeManyTracks:
		// Multiple tracks, same codec. Each track is:
		//   [trackId(1B)][dataLen(3B big-endian)][trackData(dataLen bytes)]
		// Repeat until data is exhausted.
		offset := 1
		for offset < len(data) {
			// Need at least 4 bytes: 1 trackID + 3 dataLen
			if offset+4 > len(data) {
				break
			}
			trackID := data[offset]
			// 3-byte big-endian length
			dataLen := int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
			offset += 4

			end := offset + dataLen
			if end > len(data) {
				end = len(data)
			}

			track := TrackData{
				TrackID: trackID,
				Data:    data[offset:end],
			}
			msg.Tracks = append(msg.Tracks, track)
			offset = end
		}

	case MultitrackTypeManyTracksManyCodecs:
		// Multiple tracks, different codecs. Each track is:
		//   [trackId(1B)][FourCC(4B)][dataLen(3B big-endian)][trackData(dataLen bytes)]
		// Repeat until data is exhausted.
		offset := 1
		for offset < len(data) {
			// Need at least 8 bytes: 1 trackID + 4 FourCC + 3 dataLen
			if offset+8 > len(data) {
				break
			}
			trackID := data[offset]
			trackFourCC := string(data[offset+1 : offset+5])
			dataLen := int(data[offset+5])<<16 | int(data[offset+6])<<8 | int(data[offset+7])
			offset += 8

			end := offset + dataLen
			if end > len(data) {
				end = len(data)
			}

			track := TrackData{
				TrackID: trackID,
				FourCC:  trackFourCC,
				Data:    data[offset:end],
			}
			msg.Tracks = append(msg.Tracks, track)
			offset = end
		}

	default:
		return nil, fmt.Errorf("multitrack.parse: unknown multitrack type %d", multitrackType)
	}

	return msg, nil
}
