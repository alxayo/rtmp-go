package control

// T022: Control Message Encoding
// Provides constructors for RTMP protocol control messages (types 1-6) per contracts/control.md.
// All control messages use CSID=2, MSID=0.

import (
	"encoding/binary"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// RTMP protocol control message type IDs.
const (
	TypeSetChunkSize          uint8 = 1
	TypeAbortMessage          uint8 = 2
	TypeAcknowledgement       uint8 = 3
	TypeUserControl           uint8 = 4
	TypeWindowAcknowledgement uint8 = 5
	TypeSetPeerBandwidth      uint8 = 6
)

// User Control (Type 4) event type IDs (subset required for current implementation).
const (
	UCStreamBegin  uint16 = 0
	UCPingRequest  uint16 = 6
	UCPingResponse uint16 = 7
)

// newControlMessage builds a *chunk.Message with standard control channel fields.
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
