package control

// Control Message Encoding
// ========================
// RTMP control messages (types 1-6) manage the transport layer: chunk sizes,
// flow control, and stream lifecycle. They always travel on Chunk Stream ID 2
// with Message Stream ID 0 (the protocol control channel).

import (
	"encoding/binary"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// RTMP protocol control message type IDs (types 1-6 per spec).
const (
	TypeSetChunkSize          uint8 = 1 // Changes the maximum chunk payload size
	TypeAbortMessage          uint8 = 2 // Discards a partially received message
	TypeAcknowledgement       uint8 = 3 // Reports total bytes received (flow control)
	TypeUserControl           uint8 = 4 // Stream lifecycle events (begin, ping, etc.)
	TypeWindowAcknowledgement uint8 = 5 // Sets the acknowledgement window size
	TypeSetPeerBandwidth      uint8 = 6 // Limits the peer's output bandwidth
)

// User Control (Type 4) event type IDs.
const (
	UCStreamBegin  uint16 = 0 // Server tells client a stream is ready
	UCPingRequest  uint16 = 6 // Server checks if client is alive
	UCPingResponse uint16 = 7 // Client responds to a ping
)

// newControlMessage builds a chunk.Message with the standard control channel
// fields (CSID=2, MSID=0, Timestamp=0) that all control messages use.
func newControlMessage(typeID uint8, payload []byte) *chunk.Message {
	return &chunk.Message{
		CSID:            2, // protocol control channel
		Timestamp:       0, // control messages here use timestamp=0
		MessageLength:   uint32(len(payload)),
		TypeID:          typeID,
		MessageStreamID: 0, // always 0 for control
		Payload:         payload,
	}
}

// EncodeSetChunkSize creates a Type 1 Set Chunk Size control message.
func EncodeSetChunkSize(size uint32) *chunk.Message {
	var p [4]byte
	binary.BigEndian.PutUint32(p[:], size)
	return newControlMessage(TypeSetChunkSize, p[:])
}

// EncodeAbortMessage creates a Type 2 Abort Message control message (payload = CSID to abort).
func EncodeAbortMessage(csid uint32) *chunk.Message {
	var p [4]byte
	binary.BigEndian.PutUint32(p[:], csid)
	return newControlMessage(TypeAbortMessage, p[:])
}

// EncodeAcknowledgement creates a Type 3 Acknowledgement control message.
func EncodeAcknowledgement(seq uint32) *chunk.Message {
	var p [4]byte
	binary.BigEndian.PutUint32(p[:], seq)
	return newControlMessage(TypeAcknowledgement, p[:])
}

// encodeUserControl helper for User Control (Type 4) events.
func encodeUserControl(event uint16, data4 uint32, includeData bool) *chunk.Message {
	// Event types we emit here have exactly 4 bytes of data except those we purposely omit.
	if includeData {
		var payload [6]byte
		binary.BigEndian.PutUint16(payload[0:2], event)
		binary.BigEndian.PutUint32(payload[2:6], data4)
		return newControlMessage(TypeUserControl, payload[:])
	}
	var payload2 [2]byte
	binary.BigEndian.PutUint16(payload2[0:2], event)
	return newControlMessage(TypeUserControl, payload2[:])
}

// EncodeUserControlStreamBegin creates a User Control Stream Begin (event 0) message.
func EncodeUserControlStreamBegin(streamID uint32) *chunk.Message {
	return encodeUserControl(UCStreamBegin, streamID, true)
}

// EncodeUserControlPingRequest creates a Ping Request (event 6) user control message.
func EncodeUserControlPingRequest(ts uint32) *chunk.Message {
	return encodeUserControl(UCPingRequest, ts, true)
}

// EncodeUserControlPingResponse creates a Ping Response (event 7) user control message.
func EncodeUserControlPingResponse(ts uint32) *chunk.Message {
	return encodeUserControl(UCPingResponse, ts, true)
}

// EncodeWindowAcknowledgementSize creates a Type 5 Window Acknowledgement Size control message.
func EncodeWindowAcknowledgementSize(size uint32) *chunk.Message {
	var p [4]byte
	binary.BigEndian.PutUint32(p[:], size)
	return newControlMessage(TypeWindowAcknowledgement, p[:])
}

// EncodeSetPeerBandwidth creates a Type 6 Set Peer Bandwidth control message.
func EncodeSetPeerBandwidth(bandwidth uint32, limitType uint8) *chunk.Message {
	var p [5]byte
	binary.BigEndian.PutUint32(p[0:4], bandwidth)
	p[4] = limitType
	return newControlMessage(TypeSetPeerBandwidth, p[:])
}
