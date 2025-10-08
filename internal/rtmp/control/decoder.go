package control

// T023: Control Message Decoding
// Decodes RTMP control message payloads (types 1-6) per contracts/control.md.
// All validation rules implemented according to task requirements.

import (
	"encoding/binary"
	"fmt"
)

// Structured result types returned by the decoder. These mirror the logical
// protocol fields rather than exposing raw byte slices to callers.

// SetChunkSize represents a Type 1 Set Chunk Size message.
type SetChunkSize struct {
	Size uint32
}

// AbortMessage represents a Type 2 Abort Message (not explicitly required by T023
// but included for completeness / symmetry with encoder).
type AbortMessage struct {
	CSID uint32
}

// Acknowledgement represents a Type 3 Acknowledgement message.
type Acknowledgement struct {
	SequenceNumber uint32
}

// UserControl represents a Type 4 User Control message. Only a subset of
// event types are currently interpreted (0,6,7). For unknown event types the
// remaining payload (beyond the 2-byte event header) is exposed via RawData.
type UserControl struct {
	EventType uint16
	// Optional fields (only one will be relevant depending on event type)
	StreamID  uint32 // Event 0: Stream Begin
	Timestamp uint32 // Event 6/7: Ping Request / Response timestamp
	RawData   []byte // Any additional unparsed data for unknown events
}

// WindowAcknowledgementSize represents a Type 5 Window Ack Size message.
type WindowAcknowledgementSize struct {
	Size uint32
}

// SetPeerBandwidth represents a Type 6 Set Peer Bandwidth message.
type SetPeerBandwidth struct {
	Bandwidth uint32
	LimitType uint8 // 0 = Hard, 1 = Soft, 2 = Dynamic
}

// Decode decodes a control message (types 1-6) into a structured Go value.
// The caller supplies the RTMP message type ID and the raw payload bytes.
// Returns an error for malformed payloads or validation failures.
func Decode(typeID uint8, payload []byte) (any, error) { // any == interface{}
	switch typeID {
	case TypeSetChunkSize:
		if len(payload) != 4 {
			return nil, fmt.Errorf("set chunk size: expected 4 bytes got=%d", len(payload))
		}
		v := binary.BigEndian.Uint32(payload)
		if v == 0 {
			return nil, fmt.Errorf("set chunk size: size must be > 0")
		}
		if v&0x80000000 != 0 { // bit 31 must be zero per spec (31-bit value)
			return nil, fmt.Errorf("set chunk size: high bit (bit 31) must be 0 size=%d", v)
		}
		return &SetChunkSize{Size: v}, nil
	case TypeAbortMessage:
		if len(payload) != 4 {
			return nil, fmt.Errorf("abort message: expected 4 bytes got=%d", len(payload))
		}
		return &AbortMessage{CSID: binary.BigEndian.Uint32(payload)}, nil
	case TypeAcknowledgement:
		if len(payload) != 4 {
			return nil, fmt.Errorf("acknowledgement: expected 4 bytes got=%d", len(payload))
		}
		return &Acknowledgement{SequenceNumber: binary.BigEndian.Uint32(payload)}, nil
	case TypeUserControl:
		if len(payload) < 2 {
			return nil, fmt.Errorf("user control: expected at least 2 bytes got=%d", len(payload))
		}
		ev := binary.BigEndian.Uint16(payload[0:2])
		uc := &UserControl{EventType: ev}
		switch ev {
		case UCStreamBegin: // requires 4 more bytes (stream ID)
			if len(payload) != 6 { // exact length for this event per encoder
				return nil, fmt.Errorf("user control stream begin: expected 6 bytes got=%d", len(payload))
			}
			uc.StreamID = binary.BigEndian.Uint32(payload[2:6])
		case UCPingRequest, UCPingResponse: // timestamp 4 bytes
			if len(payload) != 6 {
				return nil, fmt.Errorf("user control ping: expected 6 bytes got=%d", len(payload))
			}
			uc.Timestamp = binary.BigEndian.Uint32(payload[2:6])
		default:
			// Unknown event: capture raw remainder (if any) for higher layer to decide.
			if len(payload) > 2 {
				uc.RawData = payload[2:]
			}
		}
		return uc, nil
	case TypeWindowAcknowledgement:
		if len(payload) != 4 {
			return nil, fmt.Errorf("window ack size: expected 4 bytes got=%d", len(payload))
		}
		v := binary.BigEndian.Uint32(payload)
		if v == 0 {
			return nil, fmt.Errorf("window ack size: must be > 0")
		}
		return &WindowAcknowledgementSize{Size: v}, nil
	case TypeSetPeerBandwidth:
		if len(payload) != 5 {
			return nil, fmt.Errorf("set peer bandwidth: expected 5 bytes got=%d", len(payload))
		}
		bw := binary.BigEndian.Uint32(payload[0:4])
		lt := payload[4]
		if lt > 2 { // 0=Hard 1=Soft 2=Dynamic
			return nil, fmt.Errorf("set peer bandwidth: invalid limit type=%d", lt)
		}
		return &SetPeerBandwidth{Bandwidth: bw, LimitType: lt}, nil
	default:
		return nil, fmt.Errorf("unsupported control message type id=%d", typeID)
	}
}
