package media

// ModEx (Modifier Extension) Support — E-RTMP v2
//
// ModEx is a wrapper packet type that adds modifier extensions to another
// E-RTMP packet. The most important modifier is the nanosecond timestamp
// offset, which provides sub-millisecond precision for A/V synchronization.
//
// Wire format (after the FourCC bytes in the enhanced header):
//
//	[ModExType(4 bits)][ModExDataSize(4 bits)][ModExData(N bytes)][WrappedPacket...]
//
// The ModExType tells us what kind of modifier this is:
//
//	0 = TimestampOffsetNano — nanosecond offset from the base RTMP timestamp
//
// ModExDataSize encoding:
//
//	0 = 1 byte, 1 = 2 bytes, 2 = 3 bytes, 3 = 4 bytes, 4+ = reserved

import (
	"encoding/binary"
	"fmt"
)

// ModExType identifies the kind of modifier extension.
// Currently only TimestampOffsetNano is defined in the E-RTMP v2 spec.
const (
	// ModExTypeTimestampOffsetNano provides nanosecond precision for timestamps.
	// The base RTMP timestamp gives millisecond resolution; this modifier adds
	// a sub-millisecond offset in nanoseconds (0–999999 range).
	ModExTypeTimestampOffsetNano uint8 = 0
)

// ModExMessage represents a parsed ModEx wrapper.
type ModExMessage struct {
	// Type is the modifier extension type (e.g., ModExTypeTimestampOffsetNano).
	Type uint8

	// Data is the raw modifier data bytes (1-4 bytes depending on DataSize).
	Data []byte

	// WrappedPayload is the remaining data after the ModEx header — this is
	// the actual video/audio packet that the modifier applies to.
	// The wrapped packet has the same format as a regular enhanced packet
	// (starting with FourCC), but the first byte's PacketType field has been
	// consumed by the ModEx wrapper.
	WrappedPayload []byte

	// NanosecondOffset is the parsed nanosecond timestamp offset (only valid
	// when Type == ModExTypeTimestampOffsetNano). Range: 0–999999.
	// Add this to (baseTimestamp * 1_000_000) for full nanosecond precision.
	NanosecondOffset uint32
}

// ParseModEx parses the ModEx extension data from the payload of a
// VideoPacketType.ModEx or AudioPacketType.ModEx packet.
//
// The input should be the data AFTER the FourCC bytes (i.e., what's in
// VideoMessage.Payload or AudioMessage.Payload when PacketType is "modex").
//
// Wire format:
//
//	byte 0: [ModExType:4][ModExDataSize:4]
//	bytes 1..N: ModExData (length determined by ModExDataSize field)
//	bytes N+1..: Wrapped packet (the actual media packet being modified)
func ParseModEx(data []byte) (*ModExMessage, error) {
	// We need at least 2 bytes: 1 for the type/size header + 1 for the
	// minimum modifier data (dataSizeCode=0 means 1 byte of data).
	if len(data) < 2 {
		return nil, fmt.Errorf("modex.parse: payload too short (need >= 2 bytes, got %d)", len(data))
	}

	// First byte layout: high nibble = ModExType, low nibble = ModExDataSize code.
	modExType := (data[0] >> 4) & 0x0F
	dataSizeCode := data[0] & 0x0F

	// Decode the data size: code 0=1 byte, 1=2 bytes, 2=3 bytes, 3=4 bytes.
	// Values 4+ are reserved by the spec and must be rejected.
	if dataSizeCode >= 4 {
		return nil, fmt.Errorf("modex.parse: reserved data size code %d", dataSizeCode)
	}
	dataSize := int(dataSizeCode) + 1

	// Verify we have enough data for the header byte + modifier data bytes.
	totalHeaderSize := 1 + dataSize // 1 byte for type/size + N bytes for data
	if len(data) < totalHeaderSize {
		return nil, fmt.Errorf("modex.parse: truncated (need %d bytes for header, got %d)", totalHeaderSize, len(data))
	}

	msg := &ModExMessage{
		Type:           modExType,
		Data:           data[1 : 1+dataSize],
		WrappedPayload: data[totalHeaderSize:],
	}

	// Parse type-specific data based on the modifier type.
	switch modExType {
	case ModExTypeTimestampOffsetNano:
		// The nanosecond offset is stored as a big-endian unsigned integer
		// in 1-4 bytes. Valid range is 0–999999 (< 1 millisecond).
		var nanoOffset uint32
		switch dataSize {
		case 1:
			nanoOffset = uint32(data[1])
		case 2:
			nanoOffset = uint32(binary.BigEndian.Uint16(data[1:3]))
		case 3:
			// 3-byte big-endian: manually combine the bytes.
			nanoOffset = uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
		case 4:
			nanoOffset = binary.BigEndian.Uint32(data[1:5])
		}
		msg.NanosecondOffset = nanoOffset
	}

	return msg, nil
}
