package packet

import (
	"bytes"
	"testing"
)

// TestDataPacket_RoundTrip verifies that marshaling a DataPacket and
// unmarshaling it back produces an identical packet. This is the most
// important test — if the round trip works, the wire format is correct.
func TestDataPacket_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		pkt  DataPacket
	}{
		{
			name: "basic_solo_packet",
			pkt: DataPacket{
				Header: Header{
					Timestamp:    1000,
					DestSocketID: 42,
				},
				SequenceNumber: 1,
				Position:       PositionSolo,
				InOrder:        true,
				Encryption:     EncryptionNone,
				Retransmitted:  false,
				MessageNumber:  1,
				Payload:        []byte("hello SRT"),
			},
		},
		{
			name: "max_sequence_number",
			pkt: DataPacket{
				Header: Header{
					Timestamp:    0xFFFFFFFF,
					DestSocketID: 0xFFFFFFFF,
				},
				SequenceNumber: maxSequenceNumber, // 2^31 - 1
				Position:       PositionFirst,
				InOrder:        false,
				Encryption:     EncryptionEven,
				Retransmitted:  true,
				MessageNumber:  maxMessageNumber, // 2^26 - 1
				Payload:        []byte{0xFF, 0x00, 0xAA, 0x55},
			},
		},
		{
			name: "empty_payload",
			pkt: DataPacket{
				Header: Header{
					Timestamp:    500000,
					DestSocketID: 100,
				},
				SequenceNumber: 12345,
				Position:       PositionMiddle,
				InOrder:        false,
				Encryption:     EncryptionOdd,
				Retransmitted:  false,
				MessageNumber:  99,
				Payload:        nil,
			},
		},
		{
			name: "last_position_retransmitted",
			pkt: DataPacket{
				Header: Header{
					Timestamp:    0,
					DestSocketID: 0,
				},
				SequenceNumber: 0,
				Position:       PositionLast,
				InOrder:        true,
				Encryption:     EncryptionNone,
				Retransmitted:  true,
				MessageNumber:  0,
				Payload:        []byte{0x47}, // MPEG-TS sync byte
			},
		},
		{
			name: "large_payload",
			pkt: DataPacket{
				Header: Header{
					Timestamp:    999999,
					DestSocketID: 0xDEADBEEF,
				},
				SequenceNumber: 1000000,
				Position:       PositionSolo,
				InOrder:        true,
				Encryption:     EncryptionNone,
				Retransmitted:  false,
				MessageNumber:  500000,
				Payload:        make([]byte, 1316), // 7 * 188 = typical SRT payload
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the packet to wire format.
			data, err := tt.pkt.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary failed: %v", err)
			}

			// Verify the F bit is 0 (data packet).
			if data[0]&0x80 != 0 {
				t.Error("F bit should be 0 for data packet")
			}

			// Unmarshal the wire data back into a packet.
			got, err := UnmarshalDataPacket(data)
			if err != nil {
				t.Fatalf("UnmarshalDataPacket failed: %v", err)
			}

			// Compare every field.
			if got.SequenceNumber != tt.pkt.SequenceNumber {
				t.Errorf("SequenceNumber: got %d, want %d", got.SequenceNumber, tt.pkt.SequenceNumber)
			}
			if got.Position != tt.pkt.Position {
				t.Errorf("Position: got %d, want %d", got.Position, tt.pkt.Position)
			}
			if got.InOrder != tt.pkt.InOrder {
				t.Errorf("InOrder: got %v, want %v", got.InOrder, tt.pkt.InOrder)
			}
			if got.Encryption != tt.pkt.Encryption {
				t.Errorf("Encryption: got %d, want %d", got.Encryption, tt.pkt.Encryption)
			}
			if got.Retransmitted != tt.pkt.Retransmitted {
				t.Errorf("Retransmitted: got %v, want %v", got.Retransmitted, tt.pkt.Retransmitted)
			}
			if got.MessageNumber != tt.pkt.MessageNumber {
				t.Errorf("MessageNumber: got %d, want %d", got.MessageNumber, tt.pkt.MessageNumber)
			}
			if got.Timestamp != tt.pkt.Timestamp {
				t.Errorf("Timestamp: got %d, want %d", got.Timestamp, tt.pkt.Timestamp)
			}
			if got.DestSocketID != tt.pkt.DestSocketID {
				t.Errorf("DestSocketID: got %d, want %d", got.DestSocketID, tt.pkt.DestSocketID)
			}
			if !bytes.Equal(got.Payload, tt.pkt.Payload) {
				t.Errorf("Payload mismatch: got %d bytes, want %d bytes", len(got.Payload), len(tt.pkt.Payload))
			}
		})
	}
}

// TestDataPacket_SequenceNumberOverflow verifies that MarshalBinary rejects
// sequence numbers that exceed the 31-bit maximum.
func TestDataPacket_SequenceNumberOverflow(t *testing.T) {
	pkt := DataPacket{
		SequenceNumber: maxSequenceNumber + 1, // Too large for 31 bits
	}
	_, err := pkt.MarshalBinary()
	if err == nil {
		t.Error("expected error for sequence number overflow, got nil")
	}
}

// TestDataPacket_MessageNumberOverflow verifies that MarshalBinary rejects
// message numbers that exceed the 26-bit maximum.
func TestDataPacket_MessageNumberOverflow(t *testing.T) {
	pkt := DataPacket{
		MessageNumber: maxMessageNumber + 1, // Too large for 26 bits
	}
	_, err := pkt.MarshalBinary()
	if err == nil {
		t.Error("expected error for message number overflow, got nil")
	}
}

// TestUnmarshalDataPacket_ControlBit verifies that UnmarshalDataPacket
// returns an error when given a control packet (F bit = 1).
func TestUnmarshalDataPacket_ControlBit(t *testing.T) {
	buf := make([]byte, 16)
	buf[0] = 0x80 // F=1 → control packet
	_, err := UnmarshalDataPacket(buf)
	if err == nil {
		t.Error("expected error for control packet, got nil")
	}
}

// TestUnmarshalDataPacket_TooShort verifies that UnmarshalDataPacket
// returns an error for buffers smaller than the header.
func TestUnmarshalDataPacket_TooShort(t *testing.T) {
	_, err := UnmarshalDataPacket(make([]byte, 15))
	if err == nil {
		t.Error("expected error for short buffer, got nil")
	}
}

// TestDataPacket_WireFormat verifies the exact byte layout of a known
// data packet. This ensures compatibility with other SRT implementations.
func TestDataPacket_WireFormat(t *testing.T) {
	pkt := DataPacket{
		Header: Header{
			Timestamp:    0x00000064, // 100 µs
			DestSocketID: 0x00000001,
		},
		SequenceNumber: 0x0000000A, // 10
		Position:       PositionSolo,   // 0b11
		InOrder:        true,           // 1
		Encryption:     EncryptionNone, // 0b00
		Retransmitted:  false,          // 0
		MessageNumber:  0x00000005,     // 5
	}

	data, err := pkt.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// Verify total size is exactly 16 bytes (header only, no payload).
	if len(data) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(data))
	}

	// Byte 0-3: F=0, SeqNo=10 → 0x0000000A
	if data[0] != 0x00 || data[1] != 0x00 || data[2] != 0x00 || data[3] != 0x0A {
		t.Errorf("bytes 0-3: got %02X %02X %02X %02X, want 00 00 00 0A",
			data[0], data[1], data[2], data[3])
	}

	// Byte 4-7: PP=11, O=1, KK=00, R=0, MsgNo=5
	// Binary: 11_1_00_0_00000000000000000000000101
	// = 0xE0000005
	if data[4] != 0xE0 || data[5] != 0x00 || data[6] != 0x00 || data[7] != 0x05 {
		t.Errorf("bytes 4-7: got %02X %02X %02X %02X, want E0 00 00 05",
			data[4], data[5], data[6], data[7])
	}
}
